package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tinfoilsh/tinfoil-go"
)

var loopbackBinds = map[string]bool{
	"127.0.0.1": true,
	"::1":       true,
	"localhost": true,
}

const (
	defaultListenPort uint   = 3301
	defaultListenAddr string = "127.0.0.1"
	defaultGatewayURL string = "https://inference-gateway.tinfoil.sh"
)

var (
	gatewayURL       string
	listenPort       uint
	listenAddr       string
	logFormat        string
	userCacheSecret  string
	modelPins        []string
	pinnedOnly       bool
	verbose          bool
	trace            bool
	handshake        bool
	allowedHostnames []string
)

var deprecatedAllowedOrigins []string

var rootCmd = &cobra.Command{
	Use:   "tinfoil-proxy",
	Short: "Run a local HTTP proxy to verified Tinfoil enclaves",
	RunE:  runProxy,
}

func init() {
	rootCmd.Flags().StringVar(&gatewayURL, "gateway", defaultGatewayURL, "Tinfoil gateway URL")
	addPinFlags(rootCmd.Flags(), &modelPins, &pinnedOnly)
	rootCmd.Flags().UintVarP(&listenPort, "port", "p", defaultListenPort, "Port to listen on")
	rootCmd.Flags().StringVarP(&listenAddr, "bind", "b", defaultListenAddr, "Address to bind to")
	rootCmd.Flags().StringVar(&logFormat, "log-format", "text", "Log format: text or json")
	rootCmd.Flags().StringSliceVar(&allowedHostnames, "allowed-host", nil, "Additional Host header hostname to allow (values without a port also match portless Host headers)")
	rootCmd.Flags().StringSliceVar(&deprecatedAllowedOrigins, "allowed-origin", nil, "Deprecated; all Origin header values are allowed")
	_ = rootCmd.Flags().MarkDeprecated("allowed-origin", "all Origin header values are allowed by default")
	rootCmd.Flags().StringVar(&userCacheSecret, "user-cache-secret", "", "Prompt-cache scoping secret (empty is unset; default: generated and persisted)")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.Flags().BoolVarP(&trace, "trace", "t", false, "Trace output")
	rootCmd.Flags().BoolVar(&handshake, "handshake", false, "Emit a ready line on stdout and wait for a go signal on stdin before serving (used by the Tinfoil Proxy app)")
	_ = rootCmd.Flags().MarkHidden("handshake")
}

func addPinFlags(flags *pflag.FlagSet, pins *[]string, only *bool) {
	flags.StringArrayVar(pins, "pin", nil, "Pin a model to MODEL=owner/name[@tag][@sha256:digest] (repeatable)")
	flags.BoolVar(only, "pinned-only", false, "Serve only explicitly pinned models (requires --pin)")
}

func gatewayOptions(pins []string, only bool, secret string) (tinfoil.GatewayOptions, error) {
	opts := tinfoil.GatewayOptions{
		ClientOptions:    []tinfoil.ClientOption{tinfoil.WithUserCacheSecret(secret)},
		ModelPins:        make(map[string]tinfoil.ModelPin, len(pins)),
		PinnedModelsOnly: only,
	}
	for _, value := range pins {
		model, ref, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(model) == "" || strings.TrimSpace(ref) == "" {
			return tinfoil.GatewayOptions{}, &tinfoil.ConfigurationError{Err: fmt.Errorf("invalid --pin %q: want MODEL=REF with non-empty model and reference", value)}
		}
		if _, exists := opts.ModelPins[model]; exists {
			return tinfoil.GatewayOptions{}, &tinfoil.ConfigurationError{Err: fmt.Errorf("duplicate --pin for model %q", model)}
		}
		opts.ModelPins[model] = tinfoil.ModelPin{Repo: ref}
	}
	return opts, nil
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
