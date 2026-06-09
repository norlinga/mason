package cli

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/spf13/cobra"
)

// rootOptionsFromCommand reads the global persistent flags (inherited from the
// root) off a subcommand after parsing.
func rootOptionsFromCommand(cmd *cobra.Command) rootOptions {
	flags := cmd.Flags()
	str := func(name string) string { v, _ := flags.GetString(name); return v }
	boolean := func(name string) bool { v, _ := flags.GetBool(name); return v }
	return rootOptions{
		target:  str("target"),
		json:    boolean("json"),
		dryRun:  boolean("dry-run"),
		yes:     boolean("yes"),
		quiet:   boolean("quiet"),
		verbose: boolean("verbose"),
		config:  str("config"),
	}
}

// outputOptions derives output settings from the global flags.
func outputOptions(root rootOptions) output.Options {
	return output.Options{JSON: root.json, Quiet: root.quiet, Verbose: root.verbose}
}

// resolveTarget loads config (explicit --config or discovered .mason) and selects
// the target named by --target, or the configured default.
func resolveTarget(root rootOptions) (config.ResolvedTarget, error) {
	path := root.config
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return config.ResolvedTarget{}, err
		}
		path, err = config.Discover(cwd)
		if err != nil {
			return config.ResolvedTarget{}, err
		}
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return config.ResolvedTarget{}, err
	}
	return cfg.ResolveTarget(root.target)
}

// remoteStack bundles the remote services a command composes for a target.
type remoteStack struct {
	target config.Target
	fs     remote.RemoteFS
	store  remote.ManifestStore
	rsync  *remote.Rsync
	links  *links.Service
	cache  *remote.LocalCache
}

// newRemoteStack wires the SSH-backed remote services for a resolved target. It
// performs no I/O; connections are established lazily on first use.
func newRemoteStack(resolved config.ResolvedTarget) (remoteStack, error) {
	target := resolved.Target
	ssh := remote.NewSSH(remote.SSHOptionsFromTarget(target))
	fs := remote.NewSSHFS(ssh)
	store := remote.NewRemoteManifestStore(target.Root, fs, ssh)
	cache, err := remote.NewLocalCache()
	if err != nil {
		return remoteStack{}, err
	}
	return remoteStack{
		target: target,
		fs:     fs,
		store:  store,
		rsync:  remote.NewRsync(),
		links:  links.NewService(target.Root, fs, store),
		cache:  cache,
	}, nil
}

// remoteSync runs a best-effort cache reconcile for read commands, discarding
// divergence warnings (the sync command is where those surface).
func remoteSync(ctx context.Context, stack remoteStack, targetID string) (remote.SyncResult, error) {
	return remote.Sync(ctx, stack.store, stack.cache, targetID, io.Discard)
}

// emitError renders err to the user: a JSON failure envelope on stdout under
// --json, otherwise a plain message on stderr. It returns err so the command can
// propagate a non-zero exit.
func emitError(stdout, stderr io.Writer, opts output.Options, err error) error {
	if err == nil {
		return nil
	}
	if opts.JSON {
		_ = output.WriteJSON(stdout, output.Failure(asOutputError(err)))
		return err
	}
	_, _ = io.WriteString(stderr, "error: "+err.Error()+"\n")
	return err
}

// asOutputError coerces err into an *output.Error, defaulting to an internal code.
func asOutputError(err error) *output.Error {
	var oe *output.Error
	if errors.As(err, &oe) {
		return oe
	}
	return output.NewError(output.CodeInternal, err.Error(), err)
}
