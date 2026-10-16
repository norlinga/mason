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
		json:    boolean("json"),
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

// loadConfig loads ~/.mason merged with the nearest project .mason (or --config).
func loadConfig(root rootOptions) (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return config.Load(config.LoadOptions{Dir: cwd, ProjectFile: root.config})
}

// remoteStack bundles the remote services the share commands compose for the share store.
type remoteStack struct {
	share config.Share
	// cacheID keys the local manifest cache: the share store's host.
	cacheID string
	fs      remote.RemoteFS
	store   remote.ManifestStore
	rsync   *remote.Rsync
	links   *links.Service
	cache   *remote.LocalCache
}

// shareStack resolves the share store from ~/.mason and wires its remote services. It
// performs no I/O beyond reading config; connections are made on first use.
func shareStack(root rootOptions) (remoteStack, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return remoteStack{}, err
	}
	share, err := cfg.RequireShare()
	if err != nil {
		return remoteStack{}, err
	}
	ssh := remote.NewSSH(remote.SSHOptionsFromServer(*share.Server))
	fs := remote.NewSSHFS(ssh)
	store := remote.NewRemoteManifestStore(share.Path, fs, ssh)
	cache, err := remote.NewLocalCache()
	if err != nil {
		return remoteStack{}, err
	}
	return remoteStack{
		share:   share,
		cacheID: share.Host,
		fs:      fs,
		store:   store,
		rsync:   remote.NewRsync(),
		links:   links.NewService(share.Path, fs, store),
		cache:   cache,
	}, nil
}

// remoteSync runs a best-effort cache reconcile for read commands, discarding
// divergence warnings (the sync command is where those surface).
func remoteSync(ctx context.Context, stack remoteStack, cacheID string) (remote.SyncResult, error) {
	return remote.Sync(ctx, stack.store, stack.cache, cacheID, io.Discard)
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
