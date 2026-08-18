# Adding a new hand-written command

Most commands are resolved automatically from the API spec (see
[adding-a-command.md](adding-a-command.md)). Some — `login`, `whoami`,
`init`, and future commands like `events`/`fixtures`/`listen` — are
hand-written because they don't map to a single API operation, or need
interactive behavior the spec-driven path doesn't support. This walks
through the actual pattern this codebase uses, taken directly from
`internal/cmd/auth.go` and `internal/cmd/misc.go`.

## The pattern

1. **New file in `internal/cmd/`.** One file per logical group of commands
   (`auth.go` holds `login`/`logout`/`whoami`; `misc.go` holds `open`/
   `version`) — don't add a new top-level command to an existing file that
   isn't already about the same thing.

2. **A constructor taking `*Globals`.** Every command constructor follows
   this shape:

   ```go
   func newExampleCommand(g *Globals, version string) *cobra.Command {
       return &cobra.Command{
           Use:   "example",
           Short: "One-line description",
           RunE: func(cc *cobra.Command, args []string) error {
               // ...
           },
       }
   }
   ```

   `g *Globals` is a parameter, never a package-level variable — see
   `internal/cmd/AGENTS.md`'s Pitfalls section for exactly why this matters
   and what breaks if you get it wrong.

3. **Resolve credentials via `runtimeContext`, if you need the API.**

   ```go
   rc, _, err := runtimeContext(g)
   if err != nil {
       return err
   }
   cl := client.New(client.Options{
       BaseURL: rc.BaseURL, APIKey: rc.APIKey, Version: version,
       Debug: g.Debug, DebugOut: os.Stderr,
   })
   ```

   Do not resolve credentials any other way — this is the one place
   precedence (flag → env var → keyring → config file) is applied, and
   duplicating it elsewhere risks the two paths disagreeing.

4. **Render output via `internal/output.Writer`, if you return API data.**

   ```go
   format, err := output.ParseFormat(g.Output)
   if err != nil {
       return err
   }
   w := output.Writer{Out: os.Stdout, Err: os.Stderr, Format: format}
   return w.Render(raw, output.Options{Quiet: g.Quiet})
   ```

   Never write response data directly with `fmt.Println` — it bypasses the
   stdout/stderr contract every other command follows, and breaks
   `--output json` for this command specifically.

5. **Register it in `root.go`.** Add the new command to the
   `root.AddCommand(...)` call in `NewRootCommand`. If you are adding this
   alongside other new commands landing at the same time, be careful: this
   is a single shared call, and two changes touching it independently (e.g.
   two people, or two agents, working in parallel) can produce a real merge
   conflict or a silent overwrite if not integrated carefully — this
   happened during the CLI's initial build.

## What you get for free by following this pattern

- `--profile`, `--output`, `--debug`, `--quiet`, and every other global flag
  work automatically, since they're all on the shared `*Globals`.
- Credential precedence, retry safety, and error normalization all come from
  `runtimeContext` + `client.Client`, not anything you write yourself.
- `--output json`/`yaml`/`table` all work automatically once you render
  through `output.Writer`.

## What you should NOT do

- Do not call `net/http` directly — see `internal/client/AGENTS.md`.
- Do not add a second credential-resolution path — see
  `internal/config/AGENTS.md`.
- Do not print progress or data with a bare `fmt.Println`/`fmt.Printf` to an
  unspecified stream — always be explicit about stdout vs. stderr.
