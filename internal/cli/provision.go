package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/norlinga/mason/internal/config"
	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/remote"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// provisionParams are the provision command's flags.
type provisionParams struct {
	site       string
	print      bool
	force      bool
	admin      string
	adminKey   string
	spa        bool
	domainMode string
	noWWW      bool
	reload     bool
}

type provisionOutcome struct {
	site    config.Site
	script  string
	applied bool
	output  string
	admin   string
}

type provisionJSON struct {
	Site    string `json:"site"`
	URL     string `json:"url"`
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Script  string `json:"script"`
	Applied bool   `json:"applied"`
	Output  string `json:"output,omitempty"`
}

// provisionRunFunc renders (and unless printing, applies) the provisioning script.
type provisionRunFunc func(ctx context.Context, root rootOptions, p provisionParams, confirm func(string) (bool, error)) (provisionOutcome, error)

// errHandEdited reports that the site's vhost exists but was not written by Mason.
var errHandEdited = errors.New("the vhost was hand-edited, so mason left it alone (see the diff above) — re-run with --force to replace it; the original is backed up")

func newProvisionCommand(stdout, stderr io.Writer, run provisionRunFunc) *cobra.Command {
	p := provisionParams{reload: true}
	var noReload, apex, subdomain bool
	cmd := &cobra.Command{
		Use:   "provision [site]",
		Short: "Set up a site on its server: web root and Caddy vhost",
		Long: "Create the site's web root and a Mason-managed Caddy vhost (named by the site's host),\n" +
			"then validate and reload Caddy. Runs over SSH as an admin (default root) after asking;\n" +
			"--print shows the script instead. Safe to re-run: it converges to the same state.\n\n" +
			"Apex domains (example.com) are served from the bare domain with a www→apex redirect;\n" +
			"subdomains are served as-is. A vhost you edited by hand is never replaced without --force.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			if apex && subdomain {
				return emitError(stdout, stderr, opts, output.NewError(output.CodeUsage, "--apex and --subdomain are mutually exclusive", nil))
			}
			switch {
			case apex:
				p.domainMode = "apex"
			case subdomain:
				p.domainMode = "subdomain"
			}
			if len(args) == 1 {
				p.site = args[0]
			}
			p.reload = !noReload
			outcome, err := run(cmd.Context(), root, p, terminalConfirm(cmd, stderr, root))
			if outcome.output != "" && (err != nil || !opts.JSON) {
				writeLine(stderr, outcome.output)
			}
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderProvision(stdout, stderr, opts, outcome)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&p.print, "print", false, "print the script instead of running it")
	f.BoolVar(&p.force, "force", false, "replace a hand-edited vhost (backed up first)")
	f.StringVar(&p.admin, "admin", "", "admin user to run as (default root)")
	f.StringVar(&p.adminKey, "admin-key", "", "SSH key for the admin connection (default: your normal SSH setup)")
	f.BoolVar(&p.spa, "spa", false, "fall back to /index.html for client-side routing (or set spa: true on the site)")
	f.BoolVar(&apex, "apex", false, "treat the host as an apex domain (serve apex + redirect www→apex)")
	f.BoolVar(&subdomain, "subdomain", false, "treat the host as a subdomain (no www redirect)")
	f.BoolVar(&p.noWWW, "no-www", false, "for apex domains, omit the www→apex redirect")
	f.BoolVar(&noReload, "no-reload", false, "leave validating and reloading Caddy to you")
	return cmd
}

func renderProvision(stdout, stderr io.Writer, opts output.Options, o provisionOutcome) error {
	snippet := setup.SnippetPath("", o.site.Host)
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(provisionJSON{
			Site: o.site.Name, URL: o.site.URL, Path: o.site.Path, Snippet: snippet,
			Script: o.script, Applied: o.applied, Output: o.output,
		}))
	}
	if !o.applied {
		_, err := io.WriteString(stdout, o.script)
		if err == nil && !opts.Quiet {
			fmt.Fprintln(stderr, "Review the script above; 'mason provision' (without --print) runs it as an admin.")
		}
		return err
	}
	if !opts.Quiet {
		fmt.Fprintf(stdout, "✓ Provisioned %s (%s:%s, %s)\n", o.site.URL, o.site.Server.Host, o.site.Path, snippet)
		fmt.Fprintln(stdout, "Next: mason deploy")
	}
	return nil
}

func defaultProvisionRun(ctx context.Context, root rootOptions, p provisionParams, confirm func(string) (bool, error)) (provisionOutcome, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return provisionOutcome{}, err
	}
	site, err := cfg.Site(p.site)
	if err != nil {
		return provisionOutcome{}, err
	}
	script, err := setup.ProvisionScript(setup.ProvisionParams{
		Site: site, SPA: p.spa, DomainMode: p.domainMode, NoWWW: p.noWWW, Reload: p.reload,
	})
	if err != nil {
		return provisionOutcome{}, err
	}
	outcome := provisionOutcome{site: site, script: script}
	if p.print {
		return outcome, nil
	}

	admin := adminSSH(*site.Server, p.admin, p.adminKey)
	outcome.admin = admin.Destination()
	if !root.yes {
		q := fmt.Sprintf("Provision %s on %s as %s? Creates %s and %s, then reloads Caddy.",
			site.URL, site.Server.Host, admin.User, site.Path, setup.SnippetPath("", site.Host))
		ok, err := confirm(q)
		if err != nil {
			return outcome, err
		}
		if !ok {
			return outcome, errors.New("cancelled; nothing was changed on the server")
		}
	}
	if p.force {
		script = "FORCE=1\n" + script
	}
	return applyAdminScript(ctx, admin, outcome, script)
}

func applyAdminScript(ctx context.Context, admin remote.SSHOptions, outcome provisionOutcome, script string) (provisionOutcome, error) {
	res, err := remote.NewSSH(admin).Run(ctx, script)
	outcome.output = strings.TrimSpace(res.Stdout + res.Stderr)
	if err != nil {
		if res.ExitCode == 3 {
			return outcome, errHandEdited
		}
		if res.ExitCode > 0 {
			return outcome, fmt.Errorf("provisioning script failed on %s (exit %d); its output is above", admin.Host, res.ExitCode)
		}
		return outcome, err
	}
	outcome.applied = true
	return outcome, nil
}

// adminSSH connects as the admin user with the user's normal SSH setup (agent,
// ~/.ssh/config), not Mason's deploy key, unless an admin key is given.
func adminSSH(server config.Server, admin, key string) remote.SSHOptions {
	opts := remote.SSHOptionsFromServer(server)
	opts.User = strings.TrimSpace(admin)
	if opts.User == "" {
		opts.User = "root"
	}
	opts.KeyPath = strings.TrimSpace(key)
	return opts
}

// terminalConfirm asks a yes/no question on stderr, reading the answer from stdin. With
// -y it is never called.
func terminalConfirm(cmd *cobra.Command, stderr io.Writer, root rootOptions) func(string) (bool, error) {
	return func(question string) (bool, error) {
		return confirmPrompt(stderr, cmd.InOrStdin(), question)
	}
}

func confirmPrompt(stderr io.Writer, stdin io.Reader, question string) (bool, error) {
	fmt.Fprintf(stderr, "%s [y/N] ", question)
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil && err.Error() != "unexpected newline" {
		if errors.Is(err, io.EOF) {
			return false, errors.New("no answer (stdin closed) — re-run with -y to proceed non-interactively")
		}
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func writeLine(w io.Writer, s string) {
	io.WriteString(w, s)
	if !strings.HasSuffix(s, "\n") {
		io.WriteString(w, "\n")
	}
}
