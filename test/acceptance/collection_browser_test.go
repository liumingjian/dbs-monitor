//go:build acceptance

package acceptance

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

// 浏览器层条目共用一份 go:embed 进二进制的前端产物，构建一次就够；
// 构建结果也要留住，否则第二、第三条会拿着不存在的 dist 去编译服务端。
var (
	browserWebBuild       sync.Once
	browserWebBuildErr    error
	browserWebBuildOutput []byte
)

func TestAcceptance_AC_01_F2(t *testing.T) {
	runCollectionEntry(t, "AC-01-F2", "空图表打出 13 码文案而非 0，数据新鲜度可见，时间范围经 search params 往返，并一路走到采集管理的受影响指标行", func(t *testing.T) {
		runtime := startBrowserRuntime(t, 18472)
		const role = "ac-01-f2-monitor"
		createTargetLoginRole(t, role, false)

		instanceID := createIssue60Instance(t, runtime.client, "AC-01-F2 target", "monitored", role, issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		// pg.role 任务没有能力前置，所以这张页面上既有真实出点的图，也有被权限挡住的空图。
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricReplicationRole)
		waitForIssue60MetricCode(t, runtime.client, instanceID, metric.MetricConnectionTotal, api.PERMISSIONDENIED)

		runBrowserSpec(t, runtime, "e2e/collection-empty-state.spec.ts", []string{
			"AC01F2_INSTANCE_ID=" + instanceID.String(),
		})
	})
}

func TestAcceptance_AC_07_S2(t *testing.T) {
	runCollectionEntry(t, "AC-07-S2", "配置缺失待办的三态四情形都不报假绿、不隐藏模块，且采集配置对只读运维置灰但不消失", func(t *testing.T) {
		runtime := startBrowserRuntime(t, 18473)
		const role = "ac-07-s2-limited"
		createTargetLoginRole(t, role, false)

		readyID := createIssue60Instance(t, runtime.client, "AC-07-S2 ready", "monitored", "monitored", issue60TargetPort(t))
		missingID := createIssue60Instance(t, runtime.client, "AC-07-S2 missing", "monitored", role, issue60TargetPort(t))
		unknownID := createIssue60UnreachableInstance(t, runtime.client, "AC-07-S2 unknown")
		viewer := createSecurityUser(t, runtime.client, "ac-07-s2-viewer", api.READONLY)

		waitForBrowserCapability(t, runtime.client, readyID, api.RolePgMonitor, api.PRESENT)
		waitForBrowserCapability(t, runtime.client, readyID, api.ExtPgStatStatements, api.PRESENT)
		waitForBrowserCapability(t, runtime.client, readyID, api.TopoHasSlot, api.NOTAPPLICABLE)
		waitForBrowserCapability(t, runtime.client, missingID, api.RolePgMonitor, api.MISSING)
		waitForBrowserCapability(t, runtime.client, unknownID, api.RolePgMonitor, api.UNKNOWN)

		runBrowserSpec(t, runtime, "e2e/collection-configuration-todo.spec.ts", []string{
			"AC07S2_READY_ID=" + readyID.String(),
			"AC07S2_MISSING_ID=" + missingID.String(),
			"AC07S2_UNKNOWN_ID=" + unknownID.String(),
			"AC07S2_VIEWER_USERNAME=" + viewer.User.Username,
			"AC07S2_VIEWER_PASSWORD=" + viewer.InitialPassword,
		})
	})
}

func TestAcceptance_AC_07_F2(t *testing.T) {
	runCollectionEntry(t, "AC-07-F2", "暂停期图表只说「已暂停」，实例列表标记带时长，导航角标计已暂停实例数", func(t *testing.T) {
		runtime := startBrowserRuntime(t, 18474)
		instanceID := createIssue60Instance(t, runtime.client, "AC-07-F2 target", "monitored", "monitored", issue60TargetPort(t))
		setIssue60TaskIntervals(t, runtime.client, instanceID)
		waitForIssue60MetricPoints(t, runtime.client, instanceID, metric.MetricConnectionTotal)

		reason := "AC-07-F2 暂停期图表文案"
		setPause(t, runtime.client, instanceID, true, &reason)
		assertCollectionMetricUnavailable(t, runtime.client, instanceID, metric.MetricConnectionTotal,
			time.Now().UTC().Add(-time.Minute), api.COLLECTIONPAUSED)

		runBrowserSpec(t, runtime, "e2e/collection-paused-ui.spec.ts", []string{
			"AC07F2_INSTANCE_ID=" + instanceID.String(),
			"AC07F2_INSTANCE_NAME=AC-07-F2 target",
		})
	})
}

func startBrowserRuntime(t *testing.T, port int) securityRuntime {
	t.Helper()
	buildBrowserWeb(t)
	runtime := startSecurityRuntime(t, securityServerOptions{port: port, embedWeb: true})
	loginSecurityUser(t, runtime.client, "admin", securityAdminPassword)
	return runtime
}

func buildBrowserWeb(t *testing.T) {
	t.Helper()
	browserWebBuild.Do(func() {
		command := exec.Command("npm", "run", "build")
		command.Dir = filepath.Join(repositoryRoot(t), "web")
		browserWebBuildOutput, browserWebBuildErr = command.CombinedOutput()
	})
	if browserWebBuildErr != nil {
		t.Fatalf("build embedded web application: %v\n%s", browserWebBuildErr, browserWebBuildOutput)
	}
}

func runBrowserSpec(t *testing.T, runtime securityRuntime, spec string, environment []string) {
	t.Helper()
	command := exec.Command("npm", "exec", "playwright", "test", spec)
	command.Dir = filepath.Join(repositoryRoot(t), "web")
	command.Env = append(os.Environ(),
		"E2E_BASE_URL="+runtime.baseURL,
		// playwright 的 [setup] 项目读 E2E_ADMIN_*，缺省口令与本栈不符
		"E2E_ADMIN_USERNAME=admin",
		"E2E_ADMIN_PASSWORD="+securityAdminPassword,
	)
	command.Env = append(command.Env, environment...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s Playwright path: %v\n%s", spec, err, output)
	}
}

func createTargetLoginRole(t *testing.T, role string, monitoring bool) {
	t.Helper()
	admin := openIssue60Target(t, "monitored", "monitored", "monitored")
	defer admin.Close(context.Background())
	execCollectionSQL(t, admin, fmt.Sprintf("DROP ROLE IF EXISTS %q", role))
	execCollectionSQL(t, admin, fmt.Sprintf("CREATE ROLE %q LOGIN PASSWORD 'monitored'", role))
	if monitoring {
		execCollectionSQL(t, admin, fmt.Sprintf("GRANT pg_monitor TO %q", role))
	}
	t.Cleanup(func() {
		cleanup := openIssue60Target(t, "monitored", "monitored", "monitored")
		defer cleanup.Close(context.Background())
		execCollectionSQL(t, cleanup, fmt.Sprintf("DROP ROLE IF EXISTS %q", role))
	})
}

func waitForBrowserCapability(
	t *testing.T,
	client *api.ClientWithResponses,
	instanceID uuid.UUID,
	capabilityID api.CapabilitySnapshotEntryCapabilityId,
	want api.CapabilityStatus,
) {
	t.Helper()
	eventuallyIssue60(t, capabilityRefreshTimeout, func() (bool, string) {
		entries, detail := readCollectionCapabilities(client, instanceID)
		if entries == nil {
			return false, detail
		}
		entry, exists := entries[capabilityID]
		if !exists {
			return false, fmt.Sprintf("capability snapshot omitted %s", capabilityID)
		}
		if entry.Status != want {
			return false, fmt.Sprintf("capability %s = %s, want %s", capabilityID, entry.Status, want)
		}
		return true, ""
	})
}
