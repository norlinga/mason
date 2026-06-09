package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/norlinga/mason/internal/links"
	"github.com/norlinga/mason/internal/output"
	"github.com/spf13/cobra"
)

// endpointRunFunc toggles one endpoint's state. It is the seam shared by the
// disable and enable commands so output can be tested without real SSH.
type endpointRunFunc func(ctx context.Context, root rootOptions, ref string) (links.EndpointResult, error)

type endpointJSON struct {
	Ref      string `json:"ref"`
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
	Changed  bool   `json:"changed"`
}

func newDisableCommand(stdout, stderr io.Writer, run endpointRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <ref>",
		Short: "Stop a share endpoint from resolving (reversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root, args[0])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderEndpoint(stdout, opts, args[0], res, "Disabled", "already disabled")
		},
	}
}

// renderEndpoint renders a disable/enable outcome. changedVerb is printed when the
// state changed (e.g. "Disabled"); unchangedState describes the prior no-op state.
func renderEndpoint(stdout io.Writer, opts output.Options, ref string, res links.EndpointResult, changedVerb, unchangedState string) error {
	if opts.JSON {
		return output.WriteJSON(stdout, output.Success(endpointJSON{
			Ref:      ref,
			Endpoint: res.Endpoint,
			State:    string(res.State),
			Changed:  res.Changed,
		}))
	}
	if opts.Quiet {
		return nil
	}
	if res.Changed {
		_, err := fmt.Fprintf(stdout, "%s %s\n", changedVerb, ref)
		return err
	}
	_, err := fmt.Fprintf(stdout, "%s is %s.\n", ref, unchangedState)
	return err
}

// defaultDisableRun wires the real remote stack and disables the endpoint.
func defaultDisableRun(ctx context.Context, root rootOptions, ref string) (links.EndpointResult, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return links.EndpointResult{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return links.EndpointResult{}, err
	}
	return stack.links.Disable(ctx, links.EndpointRequest{Ref: ref})
}
