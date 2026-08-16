package httpapi

import "testing"

func TestA10AlertRuleRoleMatrix(t *testing.T) {
	operations := []struct {
		name     string
		canWrite bool
	}{
		{name: "ListAlertRules"},
		{name: "ListAlertRuleTemplates"},
		{name: "CreateAlertRule", canWrite: true},
		{name: "UpdateAlertRule", canWrite: true},
		{name: "UpdateAlertRuleEnabled", canWrite: true},
		{name: "DeleteAlertRule", canWrite: true},
		{name: "CopyAlertRule", canWrite: true},
		{name: "CreateAlertRuleFromTemplate", canWrite: true},
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
		required := RequiredRoles[operation.name]
		wantRequired := "READONLY"
		if operation.canWrite {
			wantRequired = "ALERT_ADMIN"
		}
		if required != wantRequired {
			t.Errorf("operation %s requires %q, want %q", operation.name, required, wantRequired)
			continue
		}
		for _, role := range roles {
			t.Run(operation.name+"/"+role.name, func(t *testing.T) {
				allowed := roleRank(role.name) >= roleRank(required)
				want := !operation.canWrite || role.canWrite
				if allowed != want {
					t.Fatalf("role %s access to %s = %t, want %t (required role %q)",
						role.name, operation.name, allowed, want, required)
				}
			})
		}
	}
}
