package model

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// v25 引入看板协作面：注释、快照、面板库与面板级告警关联。
const (
	MaxAnnotationsPerDashboard  = 500
	MaxAnnotationTitleBytes     = 200
	MaxAnnotationTextBytes      = 2000
	MaxAnnotationTags           = 8
	MaxAnnotationTagBytes       = 64
	MaxAnnotationWindowSeconds  = 366 * 24 * 60 * 60
	MaxSnapshotsPerOwner        = 32
	MaximumSnapshotDefinition   = MaxDashboardBytes
	MaxLibraryPanelsPerOwner    = 64
	MaxLibraryPanelNameBytes    = 200
	MaxLibraryPanelDescBytes    = 512
	MaxLibraryPanelBytes        = 16 * 1024
	MaxPanelAlertRules          = 4
	AnnotationVisibilityPrivate = "private"
	AnnotationVisibilityPublic  = "public"
)

var ErrInvalidDashboardAnnotation = errors.New("invalid dashboard annotation")

var ErrInvalidDashboardSnapshot = errors.New("invalid dashboard snapshot")

var ErrInvalidLibraryPanel = errors.New("invalid dashboard library panel")

var dashboardTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// DashboardAnnotation 是落在某个看板或单个面板时间轴上的注释。
type DashboardAnnotation struct {
	ID          string    `json:"id"`
	DashboardID string    `json:"dashboard_id"`
	PanelID     string    `json:"panel_id,omitempty"`
	Title       string    `json:"title"`
	Text        string    `json:"text,omitempty"`
	Tags        []string  `json:"tags"`
	Color       string    `json:"color"`
	Visibility  string    `json:"visibility"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by,omitempty"`
	UpdatedBy   string    `json:"updated_by,omitempty"`
}

func ValidateDashboardAnnotation(value DashboardAnnotation) error {
	if !dashboardKeyPattern.MatchString(value.DashboardID) && !strings.HasPrefix(value.DashboardID, "node-") {
		return fmt.Errorf("%w: dashboard reference is invalid", ErrInvalidDashboardAnnotation)
	}
	if value.PanelID != "" && (!dashboardKeyPattern.MatchString(value.PanelID) || reservedDashboardKey(value.PanelID)) {
		return fmt.Errorf("%w: panel reference is invalid", ErrInvalidDashboardAnnotation)
	}
	if !dashboardText(value.Title, MaxAnnotationTitleBytes) {
		return fmt.Errorf("%w: title is required and bounded", ErrInvalidDashboardAnnotation)
	}
	if !validDashboardDescription(value.Text) || len(value.Text) > MaxAnnotationTextBytes {
		return fmt.Errorf("%w: text is not valid", ErrInvalidDashboardAnnotation)
	}
	if len(value.Tags) > MaxAnnotationTags {
		return fmt.Errorf("%w: too many tags", ErrInvalidDashboardAnnotation)
	}
	seen := make(map[string]bool, len(value.Tags))
	for _, tag := range value.Tags {
		if !dashboardTagPattern.MatchString(tag) || seen[tag] || len(tag) > MaxAnnotationTagBytes {
			return fmt.Errorf("%w: tags must be unique lowercase keys", ErrInvalidDashboardAnnotation)
		}
		seen[tag] = true
	}
	if !dashboardColor(value.Color) {
		return fmt.Errorf("%w: color is not in the palette", ErrInvalidDashboardAnnotation)
	}
	if !slices.Contains([]string{AnnotationVisibilityPrivate, AnnotationVisibilityPublic}, value.Visibility) {
		return fmt.Errorf("%w: visibility is invalid", ErrInvalidDashboardAnnotation)
	}
	return validateAnnotationWindow(value)
}

func validateAnnotationWindow(value DashboardAnnotation) error {
	if value.From.IsZero() || value.To.IsZero() {
		return fmt.Errorf("%w: window bounds are required", ErrInvalidDashboardAnnotation)
	}
	if value.To.Before(value.From) {
		return fmt.Errorf("%w: window must not end before it starts", ErrInvalidDashboardAnnotation)
	}
	if value.To.Sub(value.From) > time.Duration(MaxAnnotationWindowSeconds)*time.Second {
		return fmt.Errorf("%w: window exceeds the maximum duration", ErrInvalidDashboardAnnotation)
	}
	return nil
}

// DashboardSnapshot 冻结某次看板定义，供只读分享。
type DashboardSnapshot struct {
	ID          string              `json:"id"`
	Token       string              `json:"token,omitempty"`
	Name        string              `json:"name"`
	DashboardID string              `json:"dashboard_id"`
	Revision    int                 `json:"revision"`
	Definition  DashboardDefinition `json:"definition"`
	ExpiresAt   *time.Time          `json:"expires_at,omitempty"`
	RevokedAt   *time.Time          `json:"revoked_at,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	CreatedBy   string              `json:"created_by,omitempty"`
}

func ValidateDashboardSnapshot(value DashboardSnapshot) error {
	if !dashboardText(value.Name, MaxLibraryPanelNameBytes) {
		return fmt.Errorf("%w: name is required and bounded", ErrInvalidDashboardSnapshot)
	}
	if !dashboardKeyPattern.MatchString(value.DashboardID) && !strings.HasPrefix(value.DashboardID, "node-") {
		return fmt.Errorf("%w: dashboard reference is invalid", ErrInvalidDashboardSnapshot)
	}
	if value.Revision < 1 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidDashboardSnapshot)
	}
	if err := ValidateDashboard(value.Definition); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDashboardSnapshot, err)
	}
	return nil
}

// DashboardLibraryPanel 是某个管理员保存的可复用单面板定义。
type DashboardLibraryPanel struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Panel       DashboardPanel `json:"panel"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func ValidateLibraryPanel(value DashboardLibraryPanel) error {
	if !dashboardText(value.Name, MaxLibraryPanelNameBytes) {
		return fmt.Errorf("%w: name is required and bounded", ErrInvalidLibraryPanel)
	}
	if !validDashboardDescription(value.Description) || len(value.Description) > MaxLibraryPanelDescBytes {
		return fmt.Errorf("%w: description is not valid", ErrInvalidLibraryPanel)
	}
	if err := ValidateDashboardPanelDefinition(value.Panel); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLibraryPanel, err)
	}
	return nil
}

// ValidateDashboardPanelDefinition 复用看板面板规则校验单个面板。
// 面板库中的系列可以引用目标看板的变量，因此这里按面板自身收集变量名。
func ValidateDashboardPanelDefinition(panel DashboardPanel) error {
	variables := make(map[string]bool, len(panel.Series))
	for _, series := range panel.Series {
		if strings.HasPrefix(series.NodeID, "$") {
			name := strings.TrimPrefix(series.NodeID, "$")
			if !dashboardVariablePattern.MatchString(name) || reservedDashboardKey(name) {
				return fmt.Errorf("%w: series variable is invalid", ErrInvalidLibraryPanel)
			}
			variables[name] = true
		}
	}
	return validateDashboardPanel(panel, variablesCopy(variables))
}

func variablesCopy(source map[string]bool) map[string]bool {
	target := make(map[string]bool, len(source))
	for key := range source {
		target[key] = true
	}
	return target
}

// DashboardPanelAlert 是看板面板与告警规则的持久化关联及其聚合状态。
type DashboardPanelAlert struct {
	DashboardID string                     `json:"dashboard_id"`
	PanelID     string                     `json:"panel_id"`
	RuleID      string                     `json:"rule_id"`
	RuleName    string                     `json:"rule_name"`
	Metric      string                     `json:"metric"`
	Severity    string                     `json:"severity"`
	Enabled     bool                       `json:"enabled"`
	CreatedAt   time.Time                  `json:"created_at"`
	State       *DashboardPanelAlertStatus `json:"state,omitempty"`
}

// DashboardPanelAlertStatus 聚合该规则下所有匹配节点的评估状态。
type DashboardPanelAlertStatus struct {
	Phase     string     `json:"phase"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"`
	Firing    int        `json:"firing"`
	Pending   int        `json:"pending"`
	NoData    int        `json:"no_data"`
	Error     int        `json:"error"`
	Normal    int        `json:"normal"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// PanelAlertPhaseRank 决定多个节点状态合成徽标时的优先级，数字越大越紧急。
func PanelAlertPhaseRank(phase, status string) int {
	switch {
	case phase == AlertPhaseFiring:
		return 6
	case phase == AlertPhasePending:
		return 5
	case status == EvaluationError:
		return 4
	case status == EvaluationNoData:
		return 3
	case phase == AlertPhaseRecovering:
		return 2
	case phase == AlertPhaseNormal:
		return 1
	default:
		return 0
	}
}
