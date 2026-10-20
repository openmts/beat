package model

import "fmt"

// validateDashboardTags 校验看板检索标签：沿用注释标签的字符集约定（小写键、每个不超过
// 64 字节），数量上限 8 且必须唯一。标签只用于看板检索，不参与查询语义。
func validateDashboardTags(tags []string) error {
	if len(tags) > MaxDashboardTags {
		return fmt.Errorf("%w: at most %d tags are allowed", ErrInvalidDashboard, MaxDashboardTags)
	}
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !dashboardTagPattern.MatchString(tag) || seen[tag] || len(tag) > MaxAnnotationTagBytes {
			return fmt.Errorf("%w: tags must be unique lowercase keys", ErrInvalidDashboard)
		}
		seen[tag] = true
	}
	return nil
}
