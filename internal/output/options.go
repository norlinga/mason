package output

// Options captures output-related global flags.
type Options struct {
	JSON    bool
	Quiet   bool
	Verbose bool
}

// DetailAllowed reports whether normal human-readable detail should be printed.
func (o Options) DetailAllowed() bool {
	return !o.Quiet
}

// DebugAllowed reports whether extra diagnostic detail should be printed.
func (o Options) DebugAllowed() bool {
	return o.Verbose && !o.Quiet
}
