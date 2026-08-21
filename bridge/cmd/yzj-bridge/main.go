package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"yzj-bridge/internal/config"
	"yzj-bridge/internal/controlapi"
	"yzj-bridge/internal/logbuf"
	"yzj-bridge/internal/paths"
	"yzj-bridge/internal/runtime"
)

type logWriter struct{ buf *logbuf.Buffer }

func (w *logWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	if msg != "" {
		w.buf.Append("INFO", "", msg)
	}
	return len(p), nil
}

func main() {
	cfgPath := flag.String("config", "", "path to config.yaml")
	controlAddr := flag.String("control-addr", "127.0.0.1:18765", "control API listen address")
	token := flag.String("token", "", "control API bearer token (auto if empty)")
	noStartWSS := flag.Bool("no-start-wss", false, "do not auto-start websocket clients")
	flag.Parse()

	_ = paths.EnsureUserData()
	defaultYAML := findDefaultYAML()
	if len(defaultYAML) > 0 {
		_ = config.BootstrapIfNeeded(defaultYAML)
	}

	path := *cfgPath
	if path == "" {
		path = paths.ConfigPath()
	}
	rt := runtime.New(path)
	if err := rt.Load(true); err != nil {
		if len(defaultYAML) > 0 {
			if repairErr := config.RepairConfigIfInvalid(defaultYAML); repairErr == nil {
				if err2 := rt.Load(true); err2 == nil {
					log.Printf("repaired invalid config at %s", path)
				} else {
					log.Fatalf("load config: %v", err2)
				}
			} else {
				log.Fatalf("load config: %v", err)
			}
		} else {
			log.Fatalf("load config: %v", err)
		}
	}
	if !*noStartWSS {
		// Load(restoreWSS) already restored; if empty map start all
		st := rt.SnapshotStatus()
		anyEnabled := false
		for _, s := range st {
			if s.WSEnabled {
				anyEnabled = true
				break
			}
		}
		if !anyEnabled && len(st) > 0 {
			rt.StartAllWSS()
		}
	}

	logs := logbuf.NewWithDir(2000, paths.LogsDir())
	defer logs.Close()
	// Avoid double timestamps: logbuf already stores Time; keep stdlog prefix-free.
	log.SetFlags(0)
	log.SetOutput(io.MultiWriter(os.Stderr, &logWriter{buf: logs}))
	stopCh := make(chan struct{})
	api := &controlapi.Server{
		RT: rt, Addr: *controlAddr, Token: *token, Logs: logs,
		OnShutdown: func() { close(stopCh) },
	}
	if api.Token == "" {
		api.Token = controlapi.NewToken()
	}
	if err := api.Start(); err != nil {
		log.Fatalf("control api: %v", err)
	}
	log.Printf("yzj-bridge running; config=%s control=http://%s", path, api.Addr)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		log.Printf("signal received, shutting down")
	case <-stopCh:
		log.Printf("shutdown via control API")
	}
	rt.Shutdown()
	api.Stop()
}

func findDefaultYAML() []byte {
	candidates := []string{
		"config.default.yaml",
		filepath.Join("binaries", "config.default.yaml"),
		filepath.Join("..", "config.default.yaml"),
		filepath.Join("..", "..", "config.default.yaml"),
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append([]string{
			filepath.Join(exeDir, "config.default.yaml"),
			filepath.Join(exeDir, "binaries", "config.default.yaml"),
		}, candidates...)
	}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if _, err := config.ParseYAML(b); err == nil {
			return b
		}
	}
	if valid, err := config.ValidatedDefaultYAML(nil); err == nil {
		return valid
	}
	return nil
}
