package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/WaelRabah/slackline/internal/auth"
	"github.com/WaelRabah/slackline/internal/config"
	"github.com/WaelRabah/slackline/internal/slack"
	"github.com/WaelRabah/slackline/internal/tui"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("slackline", flag.ContinueOnError)
	path := flags.String("config", defaultPath, "path to config.toml")
	showVersion := flags.Bool("version", false, "print the installed version")
	demo := flags.Bool("demo", false, "run offline with sample conversations")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: slackline [--config path] [--demo] [auth login [options]|auth check]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command line; use --help")
	}
	if *showVersion {
		fmt.Println("Slackline " + version)
		return nil
	}
	args := flags.Args()
	command := ""
	if len(args) >= 2 {
		command = strings.Join(args[:2], " ")
	} else if len(args) != 0 {
		command = args[0]
	}
	if command != "" && command != "auth login" && command != "auth check" {
		return errors.New("unknown command; use --help")
	}
	if *demo && command != "" {
		return errors.New("--demo cannot be used with auth commands")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "auth login" {
		return login(ctx, *path, args[2:])
	}
	if command == "auth check" && len(args) != 2 {
		return errors.New("auth check takes no additional arguments")
	}
	var api slack.API
	var cfg config.Config
	if *demo {
		api = slack.NewDemo()
	} else {
		cfg, err = config.Load(*path)
		if err != nil {
			return err
		}
		creds, err := cfg.Credentials()
		if err != nil {
			return err
		}
		api = slack.NewClient(creds)
	}
	session, err := api.AuthTest(ctx)
	if err != nil {
		return err
	}
	if command == "auth check" {
		fmt.Println("Slack session is valid.")
		return nil
	}
	opts := tui.Options{ConfigPath: *path, Provider: cfg.AI, Theme: cfg.UI.Theme, SoundEnabled: cfg.Notifications.Enabled, RecentDays: cfg.Sidebar.RecentDays}
	if !*demo {
		opts.StatePath = tui.StatePath(*path, session.TeamID, session.UserID)
	}
	program := tea.NewProgram(tui.New(ctx, api, session, *demo, opts), tea.WithContext(ctx))
	if live, ok := api.(slack.LiveAPI); ok {
		go slack.RunLive(ctx, live, func(event slack.LiveEvent) { program.Send(event) })
	}
	final, err := program.Run()
	cancel()
	if model, ok := final.(tui.Model); ok {
		if stateErr := model.SaveState(); stateErr != nil {
			return stateErr
		}
	}
	if err != nil {
		return errors.New("terminal client stopped; check terminal support and size")
	}
	return nil
}

type loginOptions struct {
	manual  bool
	browser auth.Options
}

func parseLoginOptions(args []string) (loginOptions, error) {
	var opts loginOptions
	flags := flag.NewFlagSet("auth login", flag.ContinueOnError)
	flags.BoolVar(&opts.browser.ArcExtension, "arc-extension", false, "use the optional legacy Arc helper extension instead of browser-approved debugging")
	flags.BoolVar(&opts.manual, "manual", false, "paste session credentials into hidden terminal prompts")
	flags.StringVar(&opts.browser.Workspace, "workspace", "", "Slack workspace name or https://name.slack.com")
	flags.StringVar(&opts.browser.BrowserPath, "browser", "", "Chrome-compatible browser executable")
	flags.DurationVar(&opts.browser.Timeout, "timeout", auth.DefaultTimeout, "time allowed for browser sign-in")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, errors.New("unexpected login arguments; use auth login --help")
	}
	if opts.browser.Timeout <= 0 {
		return opts, errors.New("login timeout must be positive")
	}
	if opts.manual && (opts.browser.Workspace != "" || opts.browser.BrowserPath != "" || opts.browser.ArcExtension) {
		return opts, errors.New("--manual cannot be combined with --workspace, --browser, or --arc-extension")
	}
	if _, err := auth.SignInURL(opts.browser.Workspace); err != nil {
		return opts, err
	}
	return opts, nil
}

func login(ctx context.Context, path string, args []string) error {
	opts, err := parseLoginOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	var creds slack.Credentials
	if opts.manual {
		creds, err = manualCredentials(ctx)
	} else {
		fmt.Println("Opening your browser for Slack sign-in.")
		fmt.Println("Follow the browser instructions, choose your workspace, and open Slack in the browser.")
		fmt.Println("The client will capture this session automatically. Ctrl+C cancels; --manual allows pasting credentials.")
		creds, err = auth.Browser(ctx, opts.browser)
	}
	if err != nil {
		return err
	}
	fmt.Println("Slack session captured; verifying…")
	return saveLogin(ctx, path, creds)
}

func manualCredentials(ctx context.Context) (slack.Credentials, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return slack.Credentials{}, errors.New("auth login --manual requires an interactive terminal; alternatively set SLACK_TOKEN and SLACK_COOKIE")
	}
	fmt.Println("Copy the xoxc token and d cookie from the same signed-in Slack session.")
	token, err := readSecret(ctx, fd, "Slack xoxc token (hidden): ")
	if err != nil {
		return slack.Credentials{}, err
	}
	defer clear(token)
	cookie, err := readSecret(ctx, fd, "Slack d cookie value (hidden): ")
	if err != nil {
		return slack.Credentials{}, err
	}
	defer clear(cookie)
	return slack.NewCredentials(string(token), string(cookie))
}

func saveLogin(ctx context.Context, path string, creds slack.Credentials) error {
	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := slack.NewClient(creds).AuthTest(verifyCtx); err != nil {
		return err
	}
	if err := config.SaveSession(path, creds); err != nil {
		return err
	}
	fmt.Println("Slack session verified and saved with owner-only permissions.")
	return nil
}

// Set raw mode before starting the reader so cancellation always restores echo.
// The terminal reader handles Ctrl+C as input; external signals cancel the context.
func readSecret(ctx context.Context, fd int, prompt string) ([]byte, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, errors.New("cannot prepare terminal for hidden input")
	}
	defer term.Restore(fd, state)
	defer fmt.Fprint(os.Stderr, "\r\n")
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stderr}, "")
	type result struct {
		value []byte
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := terminal.ReadPassword(prompt)
		if errors.Is(err, term.ErrPasteIndicator) {
			err = nil
		}
		done <- result{value: []byte(value), err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			clear(result.value)
			return nil, errors.New("cannot read session credential")
		}
		return result.value, nil
	}
}
