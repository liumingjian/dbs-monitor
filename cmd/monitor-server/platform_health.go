package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func startupFailureHandler(health *platformhealth.Store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/diagnostics/health" {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			if err := json.NewEncoder(writer).Encode(health.Current()); err != nil {
				log.Printf("monitor-server: encode startup health response: %v", err)
			}
			return
		}
		writePlatformFailure(writer, request)
	})
}

func reportConfigPermissions(
	health *platformhealth.Store,
	logger *log.Logger,
	path string,
	permissionsSecure bool,
	now time.Time,
) {
	if permissionsSecure {
		return
	}
	logger.Printf("monitor-server: config file %s permissions are not 0600; continuing with degraded health", path)
	process := health.Source(platformhealth.SourceServerProcess)
	process.Status = platformhealth.StatusDegraded
	process.Code = "CONFIG_FILE_PERMISSIONS_INSECURE"
	health.Update(now, process)
}

func refreshPlatformDatabaseHealth(ctx context.Context, platform *db.Pool, health *platformhealth.Store, now time.Time) {
	err := platform.Ping(ctx)
	if err == nil && health.Source(platformhealth.SourcePlatformDatabase).Status == platformhealth.StatusDegraded {
		return
	}
	health.Update(now, platformhealth.DatabaseSource(err))
}

func platformFailureHandler(next http.Handler, health *platformhealth.Store) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		databaseFailed := health.Source(platformhealth.SourcePlatformDatabase).Status == platformhealth.StatusFailed
		if !databaseFailed || strings.HasPrefix(request.URL.Path, "/api/v1/diagnostics/") {
			next.ServeHTTP(writer, request)
			return
		}
		writePlatformFailure(writer, request)
	})
}

func writePlatformFailure(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-DBS-Platform-Fault", "true")
	if strings.HasPrefix(request.URL.Path, "/api/") {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"error":{"code":"INTERNAL","message":"平台自身故障"}}`))
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusServiceUnavailable)
	_, _ = writer.Write([]byte(platformFailurePage))
}

const platformFailurePage = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>平台自身故障 - DBS Monitor</title>
  <style>
    html,body{margin:0;min-height:100%;font-family:Inter,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;background:#f7f8fa;color:#17191c}
    main{max-width:680px;margin:0 auto;padding:clamp(72px,14vh,144px) 24px}
    .status{margin:0 0 12px;color:#b42318;font-size:14px;font-weight:700}
    h1{margin:0 0 20px;font-size:clamp(30px,5vw,46px);line-height:1.15;letter-spacing:0}
    p{max-width:600px;margin:0;color:#4b5563;font-size:17px;line-height:1.7}
    hr{margin:36px 0 20px;border:0;border-top:1px solid #d8dce2}
    small{color:#667085}
  </style>
</head>
<body>
  <main>
    <p class="status">平台自身故障</p>
    <h1>控制面暂时不可用</h1>
    <p>平台无法可靠读取自身运行事实。当前页面不代表任何目标数据库的健康状态，请先恢复平台服务后再刷新。</p>
    <hr>
    <small>DBS Monitor platform fault</small>
  </main>
</body>
</html>`
