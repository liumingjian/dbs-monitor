// Package securitymodel contains the frozen authorization and attribution tables.
package securitymodel

const AuthenticatedShell = "authenticated-shell"

// RoleDecision records the result for each of the three interactive roles.
type RoleDecision struct {
	Readonly      bool
	AlertAdmin    bool
	PlatformAdmin bool
}

// PageAuthorization records visibility and every source file contributing controls to a page.
type PageAuthorization struct {
	Path    string
	Sources []string
	Visible RoleDecision
}

// PageWrite records the complete three-role decision for one page write operation.
type PageWrite struct {
	PagePath string
	Method   string
	APIPath  string
	Allowed  RoleDecision
}

var (
	allRoles      = RoleDecision{Readonly: true, AlertAdmin: true, PlatformAdmin: true}
	alertAdmins   = RoleDecision{AlertAdmin: true, PlatformAdmin: true}
	platformAdmin = RoleDecision{PlatformAdmin: true}
)

// PageAuthorizations is the page-visibility half of A10. Every role can see every registered page.
var PageAuthorizations = []PageAuthorization{
	{Path: AuthenticatedShell, Sources: []string{"web/src/routes/root.tsx"}, Visible: allRoles},
	{Path: "/login", Sources: []string{"web/src/routes/login.tsx"}, Visible: allRoles},
	{Path: "/instances", Sources: []string{"web/src/routes/instances/index.tsx"}, Visible: allRoles},
	{Path: "/instances/$id", Sources: []string{"web/src/routes/instances.$id/index.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/monitoring", Sources: []string{"web/src/routes/instances.$id/monitoring.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/performance-events", Sources: []string{"web/src/routes/instances.$id/performanceEventsPage.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/performance-events/$eventId", Sources: []string{"web/src/routes/instances.$id/performanceEventDetail.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/alerts", Sources: []string{"web/src/routes/instances.$id/alerts.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/alerts/$alertId", Sources: []string{"web/src/routes/instances.$id/alerts.$alertId.tsx", "web/src/routes/instances.$id/alertEvidence.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/alerts/rules", Sources: []string{"web/src/routes/instances.$id/alerts/rules.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/collection", Sources: []string{"web/src/routes/instances.$id/collection.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/settings", Sources: []string{"web/src/routes/instances.$id/settings.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/sessions", Sources: []string{"web/src/routes/instances.$id/sessions.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/sessions/long-query-samples", Sources: []string{"web/src/routes/instances.$id/longQuerySamples.tsx"}, Visible: allRoles},
	{Path: "/instances/$id/sessions/query-statistics", Sources: []string{"web/src/routes/instances.$id/queryStatisticsPage.tsx"}, Visible: allRoles},
	{Path: "/alerts", Sources: []string{"web/src/routes/alerts/index.tsx"}, Visible: allRoles},
	{Path: "/users", Sources: []string{"web/src/routes/users/index.tsx"}, Visible: allRoles},
	{Path: "/alert-settings/notifications", Sources: []string{"web/src/routes/alert-settings/notifications.tsx"}, Visible: allRoles},
	{Path: "/alert-settings/contacts", Sources: []string{"web/src/routes/alert-settings/contacts.tsx"}, Visible: allRoles},
	{Path: "/alert-settings/policies", Sources: []string{"web/src/routes/alert-settings/policies.tsx"}, Visible: allRoles},
	{Path: "/alert-settings/maintenance-windows", Sources: []string{"web/src/routes/alert-settings/maintenance.tsx"}, Visible: allRoles},
	{Path: "/alert-settings/maintenance-windows/new", Sources: []string{"web/src/routes/alert-settings/maintenance.tsx"}, Visible: allRoles},
}

// PageWrites is the write-capability half of A10.
var PageWrites = []PageWrite{
	{PagePath: AuthenticatedShell, Method: "PUT", APIPath: "/api/v1/password", Allowed: allRoles},
	{PagePath: "/login", Method: "POST", APIPath: "/api/v1/login", Allowed: allRoles},
	{PagePath: "/instances", Method: "POST", APIPath: "/api/v1/instances", Allowed: platformAdmin},

	{PagePath: "/users", Method: "POST", APIPath: "/api/v1/users", Allowed: platformAdmin},
	{PagePath: "/users", Method: "PUT", APIPath: "/api/v1/users/{id}/status", Allowed: platformAdmin},
	{PagePath: "/users", Method: "PUT", APIPath: "/api/v1/users/{id}/role", Allowed: platformAdmin},
	{PagePath: "/users", Method: "POST", APIPath: "/api/v1/users/{id}/password", Allowed: platformAdmin},

	{PagePath: "/instances/$id/alerts/$alertId", Method: "PUT", APIPath: "/api/v1/alert-instances/{id}/disposition", Allowed: alertAdmins},
	{PagePath: "/instances/$id/alerts/rules", Method: "POST", APIPath: "/api/v1/alert-rules", Allowed: alertAdmins},
	{PagePath: "/instances/$id/alerts/rules", Method: "PUT", APIPath: "/api/v1/alert-rules/{id}", Allowed: alertAdmins},
	{PagePath: "/instances/$id/alerts/rules", Method: "PUT", APIPath: "/api/v1/alert-rules/{id}/enabled", Allowed: alertAdmins},
	{PagePath: "/instances/$id/alerts/rules", Method: "POST", APIPath: "/api/v1/alert-rules/{id}/copies", Allowed: alertAdmins},
	{PagePath: "/instances/$id/alerts/rules", Method: "POST", APIPath: "/api/v1/alert-rule-templates/{id}/alert-rules", Allowed: alertAdmins},

	{PagePath: "/instances/$id/collection", Method: "PUT", APIPath: "/api/v1/instances/{id}/collection/tasks/{task_id}", Allowed: platformAdmin},
	{PagePath: "/instances/$id/collection", Method: "PUT", APIPath: "/api/v1/instances/{id}/collection/pause", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "PUT", APIPath: "/api/v1/instances/{id}", Allowed: alertAdmins},
	{PagePath: "/instances/$id/settings", Method: "PUT", APIPath: "/api/v1/instances/{id}/credentials", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "POST", APIPath: "/api/v1/instances/{id}/agent/registration", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "POST", APIPath: "/api/v1/instances/{id}/agent/token/rotation", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "POST", APIPath: "/api/v1/instances/{id}/agent/token/revocation", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "POST", APIPath: "/api/v1/instances/{id}/agent/disable", Allowed: platformAdmin},
	{PagePath: "/instances/$id/settings", Method: "DELETE", APIPath: "/api/v1/instances/{id}", Allowed: platformAdmin},

	{PagePath: "/alert-settings/notifications", Method: "PUT", APIPath: "/api/v1/notification-channels/smtp", Allowed: alertAdmins},
	{PagePath: "/alert-settings/notifications", Method: "POST", APIPath: "/api/v1/notification-channels/smtp/test", Allowed: alertAdmins},
	{PagePath: "/alert-settings/notifications", Method: "POST", APIPath: "/api/v1/notification-channels/webhooks", Allowed: alertAdmins},
	{PagePath: "/alert-settings/notifications", Method: "PUT", APIPath: "/api/v1/notification-channels/webhooks/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/notifications", Method: "DELETE", APIPath: "/api/v1/notification-channels/webhooks/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/notifications", Method: "POST", APIPath: "/api/v1/notification-channels/webhooks/{id}/test", Allowed: alertAdmins},

	{PagePath: "/alert-settings/contacts", Method: "POST", APIPath: "/api/v1/notification-contacts", Allowed: alertAdmins},
	{PagePath: "/alert-settings/contacts", Method: "PUT", APIPath: "/api/v1/notification-contacts/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/contacts", Method: "DELETE", APIPath: "/api/v1/notification-contacts/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/contacts", Method: "POST", APIPath: "/api/v1/notification-contact-groups", Allowed: alertAdmins},
	{PagePath: "/alert-settings/contacts", Method: "PUT", APIPath: "/api/v1/notification-contact-groups/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/contacts", Method: "DELETE", APIPath: "/api/v1/notification-contact-groups/{id}", Allowed: alertAdmins},

	{PagePath: "/alert-settings/policies", Method: "POST", APIPath: "/api/v1/notification-policies", Allowed: alertAdmins},
	{PagePath: "/alert-settings/policies", Method: "PUT", APIPath: "/api/v1/notification-policies/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/policies", Method: "DELETE", APIPath: "/api/v1/notification-policies/{id}", Allowed: alertAdmins},

	{PagePath: "/alert-settings/maintenance-windows", Method: "POST", APIPath: "/api/v1/maintenance-windows", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows", Method: "PUT", APIPath: "/api/v1/maintenance-windows/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows", Method: "POST", APIPath: "/api/v1/maintenance-windows/{id}/end", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows", Method: "DELETE", APIPath: "/api/v1/maintenance-windows/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows/new", Method: "POST", APIPath: "/api/v1/maintenance-windows", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows/new", Method: "PUT", APIPath: "/api/v1/maintenance-windows/{id}", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows/new", Method: "POST", APIPath: "/api/v1/maintenance-windows/{id}/end", Allowed: alertAdmins},
	{PagePath: "/alert-settings/maintenance-windows/new", Method: "DELETE", APIPath: "/api/v1/maintenance-windows/{id}", Allowed: alertAdmins},
}

// AttributableWrite is one A12 write-operation group and its canonical actor facts.
type AttributableWrite struct {
	ID             string
	Description    string
	ActorLocations []string
}

// AttributableWrites is the complete A12 registry from the production security boundary.
var AttributableWrites = []AttributableWrite{
	{ID: "login_success", Description: "successful login", ActorLocations: []string{"platform_event.actor_id"}},
	{ID: "login_failure", Description: "failed login", ActorLocations: []string{"platform_event.actor_subject"}},
	{ID: "user_create", Description: "create user", ActorLocations: []string{"app_user.created_by", "platform_event.actor_id"}},
	{ID: "user_status_change", Description: "disable or enable user", ActorLocations: []string{"app_user.enabled_updated_by", "platform_event.actor_id"}},
	{ID: "user_role_change", Description: "change user role", ActorLocations: []string{"app_user.role_updated_by", "platform_event.actor_id"}},
	{ID: "user_password_reset", Description: "reset another user's password", ActorLocations: []string{"platform_event.actor_id"}},
	{ID: "instance_create", Description: "create instance", ActorLocations: []string{"instance.created_by"}},
	{ID: "instance_credential_update", Description: "update instance credentials", ActorLocations: []string{"instance.credential_updated_by", "platform_event.actor_id"}},
	{ID: "instance_remove", Description: "remove instance", ActorLocations: []string{"platform_event.actor_id"}},
	{ID: "alert_rule_or_disposition_change", Description: "change alert rule, acknowledge, or ignore", ActorLocations: []string{"alert_rule.created_by", "alert_rule.updated_by", "alert_rule.enabled_updated_by", "alert_event.actor_id"}},
	{ID: "collection_pause_or_key_rotation", Description: "pause or resume collection, or rotate the master key", ActorLocations: []string{"instance_collection_config.collection_pause_updated_by", "alert_event.actor_id", "platform_event.actor_subject"}},
}
