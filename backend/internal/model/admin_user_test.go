package model

import "testing"

func TestAdminUserNormalizeAndValidate(t *testing.T) {
	user := AdminUser{Username: "  Owner.One  ", DisplayName: "  Primary owner ", Role: AdminRoleOwner, Enabled: true}
	user.Normalize()
	if user.Username != "owner.one" || user.DisplayName != "Primary owner" {
		t.Fatalf("normalized user = %#v", user)
	}
	if err := user.Validate(); err != nil {
		t.Fatalf("validate user: %v", err)
	}

	invalid := []AdminUser{
		{Username: "ab", DisplayName: "Owner", Role: AdminRoleOwner},
		{Username: "bad name", DisplayName: "Owner", Role: AdminRoleOwner},
		{Username: "owner", DisplayName: "", Role: AdminRoleOwner},
		{Username: "owner", DisplayName: "Owner", Role: "root"},
	}
	for _, candidate := range invalid {
		candidate.Normalize()
		if err := candidate.Validate(); err == nil {
			t.Fatalf("Validate(%#v) succeeded", candidate)
		}
	}
}

func TestValidateAdminPassword(t *testing.T) {
	for _, password := range []string{"short", string(make([]byte, 129))} {
		if err := ValidateAdminPassword(password); err == nil {
			t.Fatalf("ValidateAdminPassword length %d succeeded", len(password))
		}
	}
	if err := ValidateAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("ValidateAdminPassword: %v", err)
	}
}
