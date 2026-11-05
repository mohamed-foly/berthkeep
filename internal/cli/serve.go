package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/berthkeep/berthkeep/internal/app"
	"github.com/berthkeep/berthkeep/internal/server"
)

func init() {
	register("serve", "run the background service in the foreground: serve [--port n]", runServe)
}

func runServe(e *Env, args []string) error {
	fs := newFlags("serve", e)
	port := fs.Int("port", 0, "API/UI port (default from config)")
	if _, err := parseInterleaved(fs, args); err != nil {
		return errUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return app.Serve(ctx, app.ServeOptions{Home: e.Home, Port: *port, Version: Version, Ready: func(s *server.Server) {
		fmt.Fprintf(e.Out, "berthkeep service on http://127.0.0.1:%d (Ctrl+C to stop)\n", s.Port)
	}})
}
