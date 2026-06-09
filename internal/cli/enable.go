package cli

import (
	"context"
	"io"

	"github.com/norlinga/mason/internal/links"
	"github.com/spf13/cobra"
)

func newEnableCommand(stdout, stderr io.Writer, run endpointRunFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <ref>",
		Short: "Re-activate a disabled share endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := rootOptionsFromCommand(cmd)
			opts := outputOptions(root)
			res, err := run(cmd.Context(), root, args[0])
			if err != nil {
				return emitError(stdout, stderr, opts, err)
			}
			return renderEndpoint(stdout, opts, args[0], res, "Enabled", "already enabled")
		},
	}
}

// defaultEnableRun wires the real remote stack and enables the endpoint.
func defaultEnableRun(ctx context.Context, root rootOptions, ref string) (links.EndpointResult, error) {
	resolved, err := resolveTarget(root)
	if err != nil {
		return links.EndpointResult{}, err
	}
	stack, err := newRemoteStack(resolved)
	if err != nil {
		return links.EndpointResult{}, err
	}
	return stack.links.Enable(ctx, links.EndpointRequest{Ref: ref})
}
