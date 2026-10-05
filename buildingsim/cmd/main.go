package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	buildingsim "github.com/eislab-cps/buildingsim"
	"github.com/eislab-cps/buildingsim/pkg/server"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "buildsim",
	Short: "Building simulation server",
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the BuildSim server",
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		host, _ := cmd.Flags().GetString("host")
		allInterfaces, _ := cmd.Flags().GetBool("all-interfaces")
		var err error
		host, err = resolveListenHost(host, allInterfaces, cmd.Flags().Changed("host"))
		if err != nil {
			return err
		}
		ui, _ := cmd.Flags().GetString("ui")
		edit, _ := cmd.Flags().GetBool("edit")
		editOutput, _ := cmd.Flags().GetString("edit-output")
		logFile, _ := cmd.Flags().GetString("log-file")
		srv := server.New(port, buildingsim.DataFS, buildingsim.WebFS, edit)
		srv.Host = host
		srv.UI = ui
		srv.EditOutputDir = editOutput
		srv.LogFile = logFile
		return srv.Start()
	},
}

func resolveListenHost(host string, allInterfaces, hostExplicit bool) (string, error) {
	if allInterfaces && hostExplicit {
		return "", fmt.Errorf("--all-interfaces cannot be combined with --host")
	}
	if allInterfaces {
		return "0.0.0.0", nil
	}
	return host, nil
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("buildsim %s (%s/%s, built %s)\n", version, runtime.GOOS, runtime.GOARCH, buildTime)
	},
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check a running BuildSim server",
	RunE: func(cmd *cobra.Command, args []string) error {
		endpoint, _ := cmd.Flags().GetString("url")
		return checkHealth(cmd, endpoint)
	},
}

func checkHealth(cmd *cobra.Command, endpoint string) error {
	requestContext := cmd.Context()
	if requestContext == nil {
		requestContext = context.Background()
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, strings.TrimSpace(endpoint), nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("BuildSim health check failed: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("BuildSim health check returned %s", response.Status)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "ok")
	return nil
}

func init() {
	startCmd.Flags().IntP("port", "p", 9090, "Port to listen on")
	startCmd.Flags().String("host", "127.0.0.1", "Host address to listen on")
	startCmd.Flags().Bool("all-interfaces", false, "Listen on all IPv4 interfaces (no authentication)")
	startCmd.Flags().String("ui", server.UIModern, "Viewer UI: modern or classic")
	startCmd.Flags().Bool("edit", false, "Enable floor plan editing tools")
	startCmd.Flags().String("edit-output", "", "Directory for persistent edit-mode exports")
	startCmd.Flags().String("log-file", "", "Optional request log file (stdout is always used)")
	healthCmd.Flags().String("url", "http://127.0.0.1:9090/healthz", "Health endpoint URL")
	rootCmd.AddCommand(startCmd, versionCmd, healthCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
