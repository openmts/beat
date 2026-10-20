package protocol

import (
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumCommandBytes         = 64 * 1024
	MaximumOutputBytes          = 1024 * 1024
	MaximumTimeoutSeconds       = 3600
	MaximumTargets              = 200
	MaximumConcurrency          = 4
	MaximumFileBytes      int64 = 2 * 1024 * 1024 * 1024
	MaximumPathBytes            = 4096
	MaximumPathComponents       = 64
)

var (
	ErrBusy        = errors.New("operation is temporarily busy")
	ErrInvalid     = errors.New("invalid operation request")
	ErrForbidden   = errors.New("operation is not authorized")
	ErrConflict    = errors.New("operation state conflict")
	ErrNotFound    = errors.New("operation not found")
	ErrUnavailable = errors.New("operations are unavailable")
)

type Root struct {
	Name         string `json:"name,omitempty"`
	ID           string `json:"id"`
	Read         bool   `json:"read"`
	Write        bool   `json:"write"`
	MaximumBytes int64  `json:"maximum_bytes"`
	Overwrite    bool   `json:"overwrite"`
	Executable   bool   `json:"executable"`
}

type Policy struct {
	Retention          *RetentionPolicy `json:"retention,omitempty"`
	SupportedShells    []string         `json:"supported_shells,omitempty"`
	Enabled            bool             `json:"enabled"`
	Kinds              []string         `json:"kinds"`
	MaximumTargets     int              `json:"maximum_targets"`
	MaximumActiveTasks int              `json:"maximum_active_tasks"`
	Concurrency        int              `json:"concurrency"`
	TimeoutSeconds     int              `json:"timeout_seconds"`
	OutputBytes        int              `json:"output_bytes"`
	AllowRoot          bool             `json:"allow_root"`
	RequireScan        bool             `json:"require_scan"`
	Roots              []Root           `json:"roots"`
}

func (policy Policy) Validate() error {
	if err := policy.validateRetention(); err != nil {
		return err
	}
	if len(policy.SupportedShells) > 2 {
		return ErrInvalid
	}
	for index, shell := range policy.SupportedShells {
		if shell != "sh" && shell != "powershell" || slices.Contains(policy.SupportedShells[:index], shell) {
			return ErrInvalid
		}
	}
	if policy.MaximumTargets < 1 || policy.MaximumTargets > MaximumTargets || policy.MaximumActiveTasks < 1 || policy.MaximumActiveTasks > 1000 {
		return ErrInvalid
	}
	if policy.Concurrency < 1 || policy.Concurrency > MaximumConcurrency || policy.TimeoutSeconds < 1 || policy.TimeoutSeconds > MaximumTimeoutSeconds {
		return ErrInvalid
	}
	if policy.OutputBytes < 1 || policy.OutputBytes > MaximumOutputBytes || len(policy.Kinds) > 4 || len(policy.Roots) > 64 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, kind := range policy.Kinds {
		if !slices.Contains([]string{"snippet", "shell", "deploy", "collect"}, kind) || seen[kind] {
			return ErrInvalid
		}
		seen[kind] = true
	}
	return validateRoots(policy.Roots)
}

func validateRoots(roots []Root) error {
	seen := map[string]bool{}
	for _, root := range roots {
		if !utf8.ValidString(root.Name) || utf8.RuneCountInString(root.Name) > 80 || strings.IndexFunc(root.Name, unicode.IsControl) >= 0 {
			return ErrInvalid
		}
		if !ValidID(root.ID) || seen[root.ID] || root.MaximumBytes < 1 || root.MaximumBytes > MaximumFileBytes {
			return ErrInvalid
		}
		seen[root.ID] = true
	}
	return nil
}

func ValidID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Authorize 验证所有硬上限后取两端交集，数据库中的宽松值不能扩大本机策略。
func Authorize(owner, local Policy, request Limits) error {
	if owner.Validate() != nil || local.Validate() != nil {
		return ErrForbidden
	}
	if !owner.Enabled || !local.Enabled || !slices.Contains(owner.Kinds, request.Kind) || !slices.Contains(local.Kinds, request.Kind) {
		return ErrForbidden
	}
	if request.Targets < 1 || request.Targets > min(owner.MaximumTargets, local.MaximumTargets) {
		return ErrForbidden
	}
	if request.TimeoutSeconds < 1 || request.TimeoutSeconds > min(owner.TimeoutSeconds, local.TimeoutSeconds) {
		return ErrForbidden
	}
	if request.OutputBytes < 1 || request.OutputBytes > min(owner.OutputBytes, local.OutputBytes) {
		return ErrForbidden
	}
	if request.RunAsRoot && (!owner.AllowRoot || !local.AllowRoot) {
		return ErrForbidden
	}
	return nil
}

type Limits struct {
	Kind           string `json:"kind"`
	Targets        int    `json:"targets"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	OutputBytes    int    `json:"output_bytes"`
	RunAsRoot      bool   `json:"run_as_root"`
}

func ValidateCommand(content []byte) error {
	if len(content) == 0 || len(content) > MaximumCommandBytes || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		return ErrInvalid
	}
	return nil
}

func ValidateRelativePath(value string) error {
	if value == "" || len(value) > MaximumPathBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:\x00") || strings.HasPrefix(value, "/") {
		return ErrInvalid
	}
	parts := strings.Split(value, "/")
	if len(parts) > MaximumPathComponents {
		return ErrInvalid
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.TrimRight(part, " .") != part {
			return ErrInvalid
		}
		if invalidComponent(part) {
			return ErrInvalid
		}
	}
	return nil
}

func invalidComponent(part string) bool {
	for _, r := range part {
		if r < 32 || r == 127 {
			return true
		}
	}
	name := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	return slices.Contains([]string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}, name)
}

func AuthorizeShell(owner, local Policy, shell string) error {
	if !slices.Contains(local.SupportedShells, shell) {
		return ErrForbidden
	}
	if len(owner.SupportedShells) > 0 && !slices.Contains(owner.SupportedShells, shell) {
		return ErrForbidden
	}
	return nil
}
