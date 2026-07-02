package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
)

const defaultPasswordEnvVar = "PASSWORD_HASH_PASSWORD"

type envLookup func(string) (string, bool)

type options struct {
	password string
	envVar   string
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.LookupEnv, stdinIsTerminal(os.Stdin)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, lookupEnv envLookup, isTerminal bool) error {
	opts, positionalArgs, err := parseOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	password, err := resolvePassword(opts, positionalArgs, stdin, stdout, lookupEnv, isTerminal)
	if err != nil {
		return err
	}

	hash, err := assistantauth.HashPassword(password)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, hash)
	return err
}

func parseOptions(args []string, stderr io.Writer) (options, []string, error) {
	fs := flag.NewFlagSet("password-hash", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts options
	fs.StringVar(&opts.password, "password", "", "password to hash")
	fs.StringVar(&opts.envVar, "env", defaultPasswordEnvVar, "environment variable name to read when no password argument is provided")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [-password <password>] [password]\n", fs.Name())
		fmt.Fprintf(fs.Output(), "\nFalls back to environment variable %q, then interactive or piped stdin.\n", defaultPasswordEnvVar)
	}

	if err := fs.Parse(args); err != nil {
		return options{}, nil, err
	}

	return opts, fs.Args(), nil
}

func resolvePassword(opts options, positionalArgs []string, stdin io.Reader, stdout io.Writer, lookupEnv envLookup, isTerminal bool) (string, error) {
	if opts.password != "" && len(positionalArgs) > 0 {
		return "", errors.New("provide the password with either -password or a positional argument")
	}
	if len(positionalArgs) > 1 {
		return "", errors.New("expected at most one positional password argument")
	}

	if opts.password != "" {
		return opts.password, nil
	}
	if len(positionalArgs) == 1 {
		return positionalArgs[0], nil
	}
	if password, ok := lookupEnv(opts.envVar); ok {
		return password, nil
	}

	if isTerminal {
		if _, err := fmt.Fprint(stdout, "Password: "); err != nil {
			return "", err
		}
		password, err := readPasswordLine(stdin)
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return "", err
		}
		return password, nil
	}

	return readPasswordStream(stdin)
}

func readPasswordLine(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", errors.New("password is required")
	}
	return password, nil
}

func readPasswordStream(stdin io.Reader) (string, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", err
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", errors.New("password is required")
	}
	return password, nil
}

func stdinIsTerminal(stdin *os.File) bool {
	info, err := stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
