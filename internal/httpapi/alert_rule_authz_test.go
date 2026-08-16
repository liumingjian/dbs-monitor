package httpapi

import "testing"

func TestAlertRuleRoleMatrix(t *testing.T) {
	operations := []struct {
		name         string
		requiredRole string
	}{
		{name: "ListAlertRules", requiredRole: "READONLY"},
		{name: "ListAlertRuleTemplates", requiredRole: "READONLY"},
		{name: "CreateAlertRule", requiredRole: "ALERT_ADMIN"},
		{name: "UpdateAlertRule", requiredRole: "ALERT_ADMIN"},
		{name: "UpdateAlertRuleEnabled", requiredRole: "ALERT_ADMIN"},
		{name: "DeleteAlertRule", requiredRole: "ALERT_ADMIN"},
		{name: "CopyAlertRule", requiredRole: "ALERT_ADMIN"},
		{name: "CreateAlertRuleFromTemplate", requiredRole: "ALERT_ADMIN"},
	}
	roles := []struct {
		name     string
		canWrite bool
	}{
		{name: "READONLY"},
		{name: "ALERT_ADMIN", canWrite: true},
		{name: "PLATFORM_ADMIN", canWrite: true},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			required, exists := RequiredRoles[operation.name]
			if !exists {
				t.Fatal("operation is missing from the required-role table")
			}
			if required != operation.requiredRole {
				t.Fatalf("required role = %q, want %q", required, operation.requiredRole)
			}

			for _, role := range roles {
				t.Run(role.name, func(t *testing.T) {
					allowed := roleRank(role.name) >= roleRank(required)
					want := operation.requiredRole == "READONLY" || role.canWrite
					if allowed != want {
						t.Fatalf("access = %t, want %t for required role %q", allowed, want, required)
					}
				})
			}
		})
	}
}
