// Package testenv brings the whole witness stack up in Docker — Postgres with
// the schema and the Grafana views, Kafka, and Grafana itself with the backend
// plugin and the trace dashboard provisioned — and runs two services against
// it that keep emitting events until stopped.
//
// It is a demo you can look at, not an assertion. Run it with:
//
//	WITNESS_TESTENV=1 go test -v -timeout 0 -run TestEnv ./testenv
//
// Both flags matter: without -timeout 0 the test panics after ten minutes,
// and without -v go test buffers the output so nothing is printed while it
// runs. Without WITNESS_TESTENV the test skips, so a plain `go test ./...`
// over the workspace does not hang forever.
package testenv

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgAlias      = "witness-pg"
	pgImage      = "postgres:16-alpine"
	kafkaImage   = "confluentinc/confluent-local:7.6.1"
	grafanaImage = "grafana/grafana:11.3.0"
	grafanaPort  = "3000/tcp"
	pluginID     = "imakiri-witness-datasource"
)

// Env is a running stack. DSN reaches Postgres from this process; Grafana
// reaches it over the Docker network under pgAlias, which is a different
// address for the same database — the two must not be swapped.
type Env struct {
	DSN        string
	Brokers    []string
	GrafanaURL string

	terminate []func(context.Context) error
}

// terminator adapts a container's Terminate to the cleanup list.
func terminator(c interface {
	Terminate(context.Context, ...testcontainers.TerminateOption) error
}) func(context.Context) error {
	return func(ctx context.Context) error { return c.Terminate(ctx) }
}

func (e *Env) Close(ctx context.Context) {
	for i := len(e.terminate) - 1; i >= 0; i-- {
		_ = e.terminate[i](ctx)
	}
}

// repoRoot is the checkout this file was compiled from. The stack mounts the
// plugin and the provisioning directory straight out of the working copy, so
// editing a view or a dashboard and restarting is the whole edit loop.
func repoRoot() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(self))
}

func Start(ctx context.Context, logf func(string, ...any)) (_ *Env, err error) {
	root := repoRoot()
	grafanaDir := filepath.Join(root, "observers", "postgres", "monitors", "grafana")

	var env = &Env{}
	defer func() {
		if err != nil {
			env.Close(context.WithoutCancel(ctx))
		}
	}()

	nw, err := network.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	env.terminate = append(env.terminate, func(context.Context) error { return nw.Remove(ctx) })

	logf("starting postgres (%s)", pgImage)
	pg, err := startPostgres(ctx, nw, grafanaDir, root)
	if err != nil {
		return nil, err
	}
	env.terminate = append(env.terminate, terminator(pg))
	host, err := pg.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		return nil, fmt.Errorf("postgres endpoint: %w", err)
	}
	env.DSN = "postgres://witness:witness@" + host + "/witness?sslmode=disable"

	logf("starting kafka (%s)", kafkaImage)
	kc, err := tckafka.Run(ctx, kafkaImage)
	if err != nil {
		return nil, fmt.Errorf("kafka: %w", err)
	}
	env.terminate = append(env.terminate, terminator(kc))
	if env.Brokers, err = kc.Brokers(ctx); err != nil {
		return nil, fmt.Errorf("kafka brokers: %w", err)
	}

	logf("building the grafana plugin")
	if err := buildPlugin(ctx, grafanaDir); err != nil {
		return nil, err
	}

	dashboards, err := stageDashboards(grafanaDir)
	if err != nil {
		return nil, err
	}

	logf("starting grafana (%s)", grafanaImage)
	gf, err := startGrafana(ctx, nw, grafanaDir, dashboards)
	if err != nil {
		return nil, err
	}
	env.terminate = append(env.terminate, terminator(gf))
	if env.GrafanaURL, err = gf.PortEndpoint(ctx, grafanaPort, "http"); err != nil {
		return nil, fmt.Errorf("grafana endpoint: %w", err)
	}

	// The plugin is what makes the waterfall render, and a plugin that failed
	// to load shows up much later as one red panel. Ask Grafana whether the
	// datasource resolved instead.
	if err := waitDatasource(ctx, env.GrafanaURL+"/api/datasources/uid/witness-plugin"); err != nil {
		return nil, err
	}
	return env, nil
}

func startPostgres(ctx context.Context, nw *testcontainers.DockerNetwork, grafanaDir, root string) (testcontainers.Container, error) {
	// Ordered, because the views are built on the tables. The postgres
	// observer's syncEventTypes also needs witness.event_types to exist
	// before any service connects, and init scripts run before we do.
	return testcontainers.Run(ctx, pgImage,
		network.WithNetwork([]string{pgAlias}, nw),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_USER":     "witness",
			"POSTGRES_PASSWORD": "witness",
			"POSTGRES_DB":       "witness",
		}),
		testcontainers.WithExposedPorts("5432/tcp"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{
				HostFilePath:      filepath.Join(root, "observers", "postgres", "000_schema.up.sql"),
				ContainerFilePath: "/docker-entrypoint-initdb.d/000_schema.up.sql",
				FileMode:          0o644,
			},
			testcontainers.ContainerFile{
				HostFilePath:      filepath.Join(grafanaDir, "views.up.sql"),
				ContainerFilePath: "/docker-entrypoint-initdb.d/001_views.up.sql",
				FileMode:          0o644,
			},
		),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			// The default 64MB /dev/shm is not enough for a parallel scan
			// over the events table once the demo has been running for a
			// minute: the trace query comes back as "could not resize shared
			// memory segment ... No space left on device".
			req.HostConfigModifier = func(hc *container.HostConfig) { hc.ShmSize = 512 << 20 }
			return nil
		}),
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
}

func startGrafana(ctx context.Context, nw *testcontainers.DockerNetwork, grafanaDir, dashboards string) (testcontainers.Container, error) {
	binds := []string{
		filepath.Join(grafanaDir, "plugin", "dist") + ":/var/lib/grafana/plugins/" + pluginID + ":ro",
		filepath.Join(grafanaDir, "provisioning") + ":/etc/grafana/provisioning:ro",
		dashboards + ":/var/lib/grafana/dashboards:ro",
	}
	return testcontainers.Run(ctx, grafanaImage,
		network.WithNetwork([]string{"witness-grafana"}, nw),
		testcontainers.WithExposedPorts(grafanaPort),
		testcontainers.WithEnv(map[string]string{
			"GF_AUTH_ANONYMOUS_ENABLED":                 "true",
			"GF_AUTH_ANONYMOUS_ORG_ROLE":                "Admin",
			"GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS": pluginID,
			"GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH": "/var/lib/grafana/dashboards/witness-trace.json",
			// Both datasources in provisioning/datasources/witness.yaml
			// interpolate this one variable — the postgres one as a bare
			// url:, the plugin one inside a postgres:// URL. So it is
			// host:port with no scheme, and it is the *container* address.
			"WITNESS_PG_URL": pgAlias + ":5432",
		}),
		testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
			req.HostConfigModifier = func(hc *container.HostConfig) {
				hc.Binds = append(hc.Binds, binds...)
			}
			return nil
		}),
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute,
			wait.ForHTTP("/api/health").WithPort(grafanaPort).WithStatusCodeMatcher(func(s int) bool { return s == http.StatusOK })),
	)
}

// buildPlugin builds the backend into dist/, where the container mounts it.
// The plugin is outside the workspace (Grafana SDK deps), hence GOWORK=off.
// Only the backend: dist/module.js is hand-written and checked in, and
// building the frontend would pull the whole create-plugin toolchain.
func buildPlugin(ctx context.Context, grafanaDir string) error {
	dir := filepath.Join(grafanaDir, "plugin")
	out := filepath.Join(dir, "dist", fmt.Sprintf("gpx_witness_%s_%s", runtime.GOOS, runtime.GOARCH))
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./pkg")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build plugin: %w: %s", err, b)
	}
	src, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return fmt.Errorf("read plugin.json: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "dist", "plugin.json"), src, 0o644)
}

// stageDashboards copies just the waterfall into a directory of its own. The
// overview dashboard is left out on purpose: two provisioned dashboards make
// it easy to end up looking at the wrong one.
func stageDashboards(grafanaDir string) (string, error) {
	dir, err := os.MkdirTemp("", "witness-dashboards-")
	if err != nil {
		return "", err
	}
	// Grafana runs as uid 472 and MkdirTemp is 0700.
	if err := os.Chmod(dir, 0o755); err != nil {
		return "", err
	}
	const name = "witness-trace.json"
	b, err := os.ReadFile(filepath.Join(grafanaDir, "dashboards", name))
	if err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, name), b, 0o644)
}

func waitDatasource(ctx context.Context, url string) error {
	deadline := time.Now().Add(time.Minute)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("status %d", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("witness plugin datasource never came up: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
