package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"open-mihomo-gateway/internal/controlapi"
	"open-mihomo-gateway/internal/webui"
)

func main() {
	configPath := flag.String("config", "examples/config.example.yaml", "path to gateway config")
	addr := flag.String("addr", "127.0.0.1:61767", "loopback listen address")
	browserOrigin := flag.String("browser-origin", "", "exact loopback browser origin when using a different SSH-forward port")
	storeDir := flag.String("store", "", "application support directory")
	helperSocket := flag.String("helper-socket", "/var/run/opensurge/helper.sock", "privileged helper socket")
	direct := flag.Bool("direct-root", false, "run actions directly; requires root and is intended for development")
	quietBootstrap := flag.Bool("quiet-bootstrap", false, "do not write a browser login link to service logs")
	flag.Parse()

	runner := controlapi.ActionRunner(controlapi.HelperClient{SocketPath: *helperSocket})
	if *direct {
		runner = controlapi.DirectRunner{}
	}
	server, err := controlapi.New(controlapi.Options{
		ConfigPath:    *configPath,
		Addr:          *addr,
		BrowserOrigin: *browserOrigin,
		StoreDir:      *storeDir,
		Runner:        runner,
		Static:        webui.Handler(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("OpenSurge Control API: %s\n", *addr)
	if !*quietBootstrap {
		fmt.Printf("Open Web GUI: %s\n", server.BootstrapURL())
	}
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
