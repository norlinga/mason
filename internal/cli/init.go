package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/norlinga/mason/internal/output"
	"github.com/norlinga/mason/internal/setup"
	"github.com/spf13/cobra"
)

// initDeps are init's collaborators: where the project is, how to ask, and how to
// provision afterwards.
type initDeps struct {
	Dir       func() (string, error)
	Provision provisionRunFunc
}

var defaultInitDeps = initDeps{Dir: os.Getwd, Provision: defaultProvisionRun}

type initJSON struct {
	Path        string `json:"path"`
	Site        string `json:"site"`
	Build       string `json:"build"`
	Provisioned bool   `json:"provisioned"`
}

func newInitCommand(stdout, stderr io.Writer, deps initDeps) *cobra.Command {
	var in setup.InitInput
	var noProvision bool
	cmd := &cobra.Command{
		Use:   "init [url]",
		Short: "Create this project's .mason (site URL + build folder), then provision it",
		Long: "Write a two-line .mason for this project: the site's URL and the folder your site\n" +
			"generator builds into. The URL defaults to the folder name when it looks like a\n" +
			"domain; the build folder is detected (dist, public, build, _site, out). Then offers\n" +
			"to provision the site on your server ('mason provision').",
		Example: "  mason init\n  mason init https://blog.example.com --build dist",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			dir, err := deps.Dir()
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if len(args) == 1 {
				in.URL = args[0]
			}
			interactive := !root.yes && !opts.JSON
			ask := newAsker(cmd.InOrStdin(), stderr)

			if in.URL == "" {
				def := domainFromFolder(dir)
				if interactive {
					if in.URL, err = ask("Site URL", def); err != nil {
						return emitError(stdout, stderr, opts, err)
					}
				} else {
					in.URL = def
				}
				if in.URL == "" {
					return emitError(stdout, stderr, opts, output.NewError(output.CodeUsage, "give the site's URL: mason init https://example.com", nil))
				}
			}
			if in.Build == "" {
				def := setup.DetectBuildDir(dir)
				if interactive {
					if def == "" {
						def = "dist"
					}
					if in.Build, err = ask("Build folder", def); err != nil {
						return emitError(stdout, stderr, opts, err)
					}
				} else {
					in.Build = def
				}
				if in.Build == "" {
					return emitError(stdout, stderr, opts, output.NewError(output.CodeUsage, "no build folder found — pass --build <dir>", nil))
				}
			}

			path, err := setup.InitProject(dir, in)
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			if !opts.JSON && !opts.Quiet {
				fmt.Fprintf(stdout, "Created %s\n", path)
			}

			provisioned := false
			if !noProvision {
				p := provisionParams{reload: true}
				prootOpts := root
				prootOpts.config = path
				outcome, err := deps.Provision(cmd.Context(), prootOpts, p, func(q string) (bool, error) {
					return confirmPrompt(stderr, cmd.InOrStdin(), q)
				})
				if outcome.output != "" && !opts.JSON {
					writeLine(stderr, outcome.output)
				}
				switch {
				case err == nil:
					provisioned = outcome.applied
					if provisioned && !opts.JSON && !opts.Quiet {
						fmt.Fprintf(stdout, "✓ Provisioned %s\nNext: mason deploy\n", outcome.site.URL)
					}
				case !opts.JSON:
					fmt.Fprintf(stderr, "Not provisioned: %v\nRun 'mason provision' when ready.\n", err)
				}
			} else if !opts.JSON && !opts.Quiet {
				fmt.Fprintln(stdout, "Next: mason provision, then mason deploy")
			}

			if opts.JSON {
				site := in.URL
				return output.WriteJSON(stdout, output.Success(initJSON{Path: path, Site: site, Build: in.Build, Provisioned: provisioned}))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&in.Build, "build", "", "folder to deploy (default: detected)")
	cmd.Flags().StringVar(&in.Server, "server", "", "server for this project (default: the one in ~/.mason)")
	cmd.Flags().BoolVar(&noProvision, "no-provision", false, "only write .mason; provision later")
	return cmd
}

// domainFromFolder suggests a URL when the project folder is named like a domain.
func domainFromFolder(dir string) string {
	name := strings.ToLower(filepath.Base(dir))
	if strings.Count(name, ".") >= 1 && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, " _/") {
		return "https://" + name
	}
	return ""
}

// newAsker returns a prompt that reads one line per question, offering a default.
func newAsker(stdin io.Reader, stderr io.Writer) func(label, def string) (string, error) {
	reader := bufio.NewReader(stdin)
	return func(label, def string) (string, error) {
		if def != "" {
			fmt.Fprintf(stderr, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(stderr, "%s: ", label)
		}
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		if line = strings.TrimSpace(line); line == "" {
			return def, nil
		}
		return line, nil
	}
}
