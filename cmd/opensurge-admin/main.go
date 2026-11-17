package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"open-mihomo-gateway/internal/config"
	"open-mihomo-gateway/internal/macosnetwork"
	"open-mihomo-gateway/internal/ubuntu"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("opensurge-admin is for Ubuntu Linux only")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("run with sudo; the console command grants administrator access")
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: sudo opensurge-admin {init|console|start|stop|status|doctor}")
	}
	path := filepath.Join(ubuntu.Root, "config.yaml")
	switch args[0] {
	case "init":
		if _, err := os.Lstat(path); err == nil {
			fmt.Println("Existing configuration retained.")
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		route, err := macosnetwork.LookupRoute(ctx, "default")
		if err != nil {
			return err
		}
		snapshot, err := macosnetwork.Discover(ctx, route.Interface, route.Interface)
		if err != nil {
			return err
		}
		cfg, err := ubuntu.InitialConfig(ubuntu.Root, snapshot)
		if err != nil {
			return err
		}
		// O_EXCL protects existing installation state even if another init races.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(f, config.Render(cfg))
		closeErr := f.Close()
		if writeErr != nil {
			_ = os.Remove(path)
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Println("Fresh Ubuntu configuration saved. Gateway is stopped; import a subscription in the Web console before starting.")
		return nil
	case "console":
		token, err := os.ReadFile(filepath.Join(ubuntu.Root, "control", "control-token"))
		if err != nil {
			return err
		}
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:61767/api/v1/session/bootstrap", strings.NewReader(`{"path":"dashboard"}`))
		req.Header.Set("Authorization", "Bearer "+string(token))
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("console login failed: HTTP %d", resp.StatusCode)
		}
		var out struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}
		u, err := url.Parse(out.URL)
		if err != nil {
			return err
		}
		u.Host = "127.0.0.1:16167"
		fmt.Println("Use an SSH tunnel: ssh -N -L 16167:127.0.0.1:61767 USER@UBUNTU_IP")
		fmt.Println("Open this one-time administrator login URL within 30 seconds (do not share it):")
		fmt.Println(u.String())
		return nil
	case "start", "stop", "status", "doctor":
		cmd := exec.Command("/usr/lib/opensurge/omg", args[0], "--config", path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
