package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// siteInitRunFunc generates (and optionally applies) the static-host provisioning
// script. It is the seam that lets output be tested without real SSH.
type siteInitRunFunc func(ctx context.Context, root rootOptions, p siteInitParams) (siteInitOutcome, error)

type siteInitParams struct {
	apply      bool
	adminUser  string
	adminKey   string
	host       string
	root       string
	spa        bool
	domainMode string
	noWWW      bool
	caddyUser  string
	group      string
	caddyfile  string
	snippetDir string
	reload     bool
}

type siteInitOutcome struct {
	script  string
	applied bool
	output  string
}

type siteInitJSON struct {
	Script  string `json:"script"`
	Applied bool   `json:"applied"`
	Output  string `json:"output,omitempty"`
}

func newSiteInitCommand(stdout, stderr io.Writer, run siteInitRunFunc) *cobra.Command {
	p := siteInitParams{reload: true}
	var noReload, apexFlag, subdomainFlag bool
	cmd := &cobra.Command{
		Use:   "site-init",
		Short: "Emit (or apply) an idempotent static-host provisioning script",
		Long: "Generate an idempotent, inspectable script that stands up a static site for the\n" +
			"target: a mason-owned, group-readable web root, Caddy read access, and a Mason-owned\n" +
			"static Caddy snippet imported once. Printed by default so you can review it; use\n" +
			"--apply --as-admin <user> to run it over SSH as an admin.\n\n" +
			"Apex domains (example.com) vs subdomains (blog.example.com) are auto-detected: an apex is\n" +
			"served from the bare domain with a permanent www→apex redirect; a subdomain is served\n" +
			"as-is. Override with --apex/--subdomain, or drop the redirect with --no-www.\n\n" +
			"Prerequisite: 'mason remote-init' (or equivalent) has already run on the host, so\n" +
			"the mason user, deploy key, and Caddyfile import discipline exist. site-init creates\n" +
			"no users, keys, or share scaffolding.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			p.reload = !noReload
			if apexFlag && subdomainFlag {
				return emitError(stdout, stderr, opts,
					output.NewError(output.CodeUsage, "--apex and --subdomain are mutually exclusive", nil))
			}
			switch {
			case apexFlag:
				p.domainMode = "apex"
			case subdomainFlag:
				p.domainMode = "subdomain"
			}
			outcome, err := run(cmd.Context(), root, p)
			if err != nil {
				// A failed --apply still captured the script's remote output; that is
				// where the actual failure diagnostics live, so surface it.
				if outcome.output != "" {
					io.WriteString(stderr, outcome.output)
					if !strings.HasSuffix(outcome.output, "\n") {
						io.WriteString(stderr, "\n")
					}
				}
				return emitError(stdout, stderr, opts, err)
			}
			return renderSiteInit(stdout, stderr, opts, outcome)
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&p.apply, "apply", false, "run the script over SSH as the admin user")
	flags.StringVar(&p.adminUser, "as-admin", "", "admin user to apply as (required with --apply)")
	flags.StringVar(&p.adminKey, "admin-key", "", "SSH key for the admin connection (default: target key_path)")
	flags.StringVar(&p.host, "host", "", "override the site host (default: host from target base_url)")
	flags.StringVar(&p.root, "root", "", "override the web root (default: deploy.remote_path)")
	flags.BoolVar(&p.spa, "spa", false, "add a try_files fallback to /index.html for SPA builds")
	flags.BoolVar(&apexFlag, "apex", false, "treat the host as an apex domain (serve apex + redirect www→apex)")
	flags.BoolVar(&subdomainFlag, "subdomain", false, "treat the host as a subdomain (no www redirect)")
	flags.BoolVar(&p.noWWW, "no-www", false, "for apex domains, omit the www→apex redirect block")
	flags.StringVar(&p.caddyUser, "caddy-user", "", "Caddy's system user (default \"caddy\")")
	flags.StringVar(&p.group, "group", "", "service group (default = remote user)")
	flags.StringVar(&p.caddyfile, "caddyfile", "", "main Caddyfile path (default /etc/caddy/Caddyfile)")
	flags.StringVar(&p.snippetDir, "snippet-dir", "", "Mason snippet directory (default /etc/caddy/mason.d)")
	flags.BoolVar(&noReload, "no-reload", false, "do not append caddy validate + reload to the script")
	return cmd
}

func renderSiteInit(stdout, stderr io.Writer, opts output.Options, outcome siteInitOutcome) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(siteInitJSON{
			Script:  outcome.script,
			Applied: outcome.applied,
			Output:  outcome.output,
		}))
	}

	if outcome.applied {
		if outcome.output != "" {
			io.WriteString(stdout, outcome.output)
			if !strings.HasSuffix(outcome.output, "\n") {
				io.WriteString(stdout, "\n")
			}
		}
		if !opts.Quiet {
			fmt.Fprintln(stderr, "✓ site-init applied")
		}
		return nil
	}

	// Default: the script is the artifact — print it verbatim for inspection/piping.
	_, err := io.WriteString(stdout, outcome.script)
	if err == nil && !opts.Quiet {
		fmt.Fprintln(stderr, "Review the script above, then apply with: mason site-init --apply --as-admin <user>")
	}
	return err
}

// defaultSiteInitRun resolves the target, generates the provisioning script, and
// applies it over SSH when requested.
func defaultSiteInitRun(ctx context.Context, root rootOptions, p siteInitParams) (siteInitOutcome, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return siteInitOutcome{}, err
	}
	target := resolved.Target

	script, err := setup.SiteInitScript(setup.SiteInitParams{
		Target:     target,
		TargetName: resolved.Name,
		Host:       p.host,
		Root:       p.root,
		SPA:        p.spa,
		DomainMode: p.domainMode,
		NoWWW:      p.noWWW,
		CaddyUser:  p.caddyUser,
		Group:      p.group,
		Caddyfile:  p.caddyfile,
		SnippetDir: p.snippetDir,
		Reload:     p.reload,
	})
	if err != nil {
		return siteInitOutcome{}, err
	}

	outcome := siteInitOutcome{script: script}
	if !p.apply {
		return outcome, nil
	}

	if strings.TrimSpace(p.adminUser) == "" {
		return outcome, output.NewError(output.CodeUsage, "--apply requires --as-admin <user>", nil)
	}
	opts := remote.SSHOptionsFromTarget(target)
	opts.User = p.adminUser
	if strings.TrimSpace(p.adminKey) != "" {
		opts.KeyPath = p.adminKey
	}
	res, runErr := remote.NewSSH(opts).Run(ctx, script)
	outcome.output = res.Stdout + res.Stderr
	if runErr != nil {
		// The session-run error echoes the whole command — here the entire script — so
		// report script failures concisely; the captured output (rendered by the command)
		// carries the diagnostics. Connect errors are already concise.
		if res.ExitCode > 0 {
			return outcome, fmt.Errorf("apply site-init: provisioning script exited %d", res.ExitCode)
		}
		return outcome, fmt.Errorf("apply site-init: %w", runErr)
	}
	outcome.applied = true
	return outcome, nil
}
