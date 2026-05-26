package main

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

var loopbackBinds = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
	"localhost": true,
}

const (
	defaultListenPort uint   = 3301
	defaultListenAddr string = "127.0.0.1"
)

var (
	enclaveHost string
	repo        string
	listenPort  uint
	listenAddr  string
	logFormat   string
	verbose     bool
	trace       bool
	handshake   bool
)

var rootCmd = &cobra.Command{
	Use:   "tinfoil-proxy",
	Short: "Run a local HTTP proxy to the verified Tinfoil enclave",
	RunE:  runProxy,
}

func init() {
	rootCmd.Flags().StringVarP(&enclaveHost, "host", "e", "", "Enclave hostname")
	rootCmd.Flags().StringVarP(&repo, "repo", "r", "", "Enclave config repo")
	rootCmd.Flags().UintVarP(&listenPort, "port", "p", defaultListenPort, "Port to listen on")
	rootCmd.Flags().StringVarP(&listenAddr, "bind", "b", defaultListenAddr, "Address to bind to")
	rootCmd.Flags().StringVar(&logFormat, "log-format", "text", "Log format: text or json")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.Flags().BoolVarP(&trace, "trace", "t", false, "Trace output")
	rootCmd.Flags().BoolVar(&handshake, "handshake", false, "Emit a ready line on stdout and wait for a go signal on stdin before serving (used by the Tinfoil Proxy app)")
	_ = rootCmd.Flags().MarkHidden("handshake")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func bindAddress() string {
	return net.JoinHostPort(listenAddr, strconv.FormatUint(uint64(listenPort), 10))
}

func warnIfNonLoopbackBind() {
	if loopbackBinds[listenAddr] {
		return
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "WARNING: tinfoil-proxy is binding to a non-loopback address.")
	fmt.Fprintf(os.Stderr, "  bind address:  %s\n", listenAddr)
	fmt.Fprintln(os.Stderr, "  The local hop between clients and this proxy is plain HTTP with no")
	fmt.Fprintln(os.Stderr, "  authentication. Anyone reachable on this interface can use your")
	fmt.Fprintln(os.Stderr, "  verified Tinfoil session. Prefer 127.0.0.1 and put a TLS-terminating")
	fmt.Fprintln(os.Stderr, "  reverse proxy in front if you need network access.")
	fmt.Fprintln(os.Stderr, "")
}
