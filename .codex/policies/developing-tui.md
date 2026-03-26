# TUI Development Policy (Bubble Tea Stack)

## Core Principles

- Build around The Elm Architecture contract (`Init`, `Update`, `View`) and keep app state centralized in the model. (Ref: [Bubble Tea TEA contract][1])
- Treat `Update` as the state transition boundary: messages in, updated model and next command out. (Ref: [Bubble Tea update signature][2])
- Prefer declarative full-view rendering each update; avoid manual redraw bookkeeping. (Ref: [Bubble Tea render model each tick][3])


## Command and Message Discipline

- Keep all I/O in commands; do not block in `Update`. (Ref: [Bubble Tea commands tutorial: I/O][4])
- Model commands as typed message producers (`type Cmd func() Msg`) with explicit message types per async flow. (Refs: [Bubble Tea `Cmd` type][5], [Bubble Tea command return messages][6])
- Use `tea.Batch` for concurrent command execution without ordering guarantees; use `tea.Sequence` when ordering is required. (Ref: [Bubble Tea batch vs sequence][7])
- `Tick`/`Every` are single-fire commands; return the next tick command from `Update` to keep loops running. (Ref: [Bubble Tea tick behavior][8])


## Runtime, Lifecycle, and Program Options

- Always process `WindowSizeMsg` and keep layout responsive; the runtime sends it initially and on resize. (Refs: [Bubble Tea window size message][9], [Bubble Tea `WindowSize` command][10])
- Use `WithContext` for external cancellation and predictable program shutdown integration. (Ref: [Bubble Tea `WithContext` option][11])
- Use `WithFilter` for cross-cutting event policy (for example, guarded quit semantics) instead of scattering conditional logic across views and components. (Ref: [Bubble Tea `WithFilter` option][12])
- Use `WithoutRenderer` only for explicit non-TUI modes (daemon/plain CLI behavior). (Ref: [Bubble Tea `WithoutRenderer` option][13])
- For interoperability, inject external events via `Program.Send`; account for startup blocking and post-exit no-op behavior. (Ref: [Bubble Tea `Program.Send` behavior][14])
- For interactive subprocesses (editor/shell), use `ExecProcess`; for non-interactive work, use regular `tea.Cmd`. (Ref: [Bubble Tea `ExecProcess` guidance][15])


## Composing with Bubbles

- Prefer Bubbles for standard interaction primitives before custom widgets. (Ref: [Bubbles README primitives][16])
- Preserve component boundaries through interfaces (`Item`, `ItemDelegate`, `help.KeyMap`) rather than coupling parent models to internals. (Refs: [Bubbles list `Item` interface][17], [Bubbles list `ItemDelegate` interface][18])
- Integrate key handling through `key.Binding` and `key.Matches`; keep keybinding and help metadata in one source of truth. (Ref: [Bubbles key matching][19])
- Leverage enabled/disabled bindings for context-sensitive controls; disabled bindings are automatically omitted from help. (Refs: [Bubbles key enabled state][20], [Bubbles help omits disabled][21])
- Wire help as a first-class model and set width explicitly so short/full help degrades gracefully on narrow terminals. (Refs: [Bubbles help width handling][22], [Bubbles short/full help][23])


## Layout and Styling with Lip Gloss

- Use Lip Gloss as the view/layout layer and Bubble Tea as the state/event runtime; keep these concerns separate. (Refs: [Lip Gloss + Bubble Tea integration][24], [Lip Gloss overview][25])
- Treat styles as immutable value objects: assign/copy freely, then derive variants via chaining and inheritance. (Refs: [Lip Gloss styles are values][26], [Lip Gloss style inheritance][27])
- Build layouts with `JoinHorizontal`, `JoinVertical`, `Place*`, and measure with `Width`/`Height`/`Size` instead of hardcoding cell math. (Refs: [Lip Gloss joins][28], [Lip Gloss placement][29], [Lip Gloss size measurement][30])
- Use `Inline`, `MaxWidth`, and `MaxHeight` when component contracts require strict rendering bounds. (Ref: [Lip Gloss width/height constraints][31])
- Rely on built-in color profile downsampling; do not add custom fallback layers unless required by product constraints. (Refs: [Lip Gloss color downsampling][32], [Lip Gloss terminal color profiles][33])
- For adaptive theming in Bubble Tea, request terminal background color and rebuild style palette on `BackgroundColorMsg`. (Ref: [Lip Gloss adaptive theming][34])


## Debugging and Observability

- Do not log to stdout while the TUI is active; use `tea.LogToFile` guarded by env flags. (Ref: [Bubble Tea log to file][35])
- For interactive debugging with Delve, run in headless mode and attach from another terminal. (Ref: [Bubble Tea delve debugging][36])

##  Reference
[1]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/README.md#L79-L85
[2]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/tea.go#L58-L64
[3]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/README.md#L206-L208
[4]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/tutorials/commands/README.md#L49-L53
[5]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/tutorials/commands/README.md#L83-L85
[6]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/tutorials/commands/README.md#L183-L186
[7]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/commands.go#L7-L25
[8]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/commands.go#L74-L77
[9]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/screen.go#L5-L12
[10]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/commands.go#L169-L172
[11]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/options.go#L19-L22
[12]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/options.go#L104-L133
[13]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/options.go#L90-L98
[14]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/tea.go#L1157-L1164
[15]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/exec.go#L28-L31
[16]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/README.md#L10-L11
[17]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/list/list.go#L33-L38
[18]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/list/list.go#L40-L61
[19]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/key/key.go#L41-L43
[20]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/key/key.go#L103-L107
[21]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/help/help.go#L16-L17
[22]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/help/help.go#L115-L117
[23]: https://github.com/charmbracelet/bubbles/blob/f1daacfa0cfee07e31a12498078426d275aa5286/help/help.go#L125-L127
[24]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L954-L960
[25]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L10-L15
[26]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L306-L307
[27]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L311-L313
[28]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L425-L433
[29]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L441-L457
[30]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L473-L487
[31]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L342-L355
[32]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L925-L927
[33]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L921-L924
[34]: https://github.com/charmbracelet/lipgloss/blob/cd93a9f5d2e3cb151da83150db29751d92585d23/README.md#L844-L856
[35]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/README.md#L297-L304
[36]: https://github.com/charmbracelet/bubbletea/blob/b427e7753962c18f1e20c9cea4fca85ae4d0cb6e/README.md#L274-L284
