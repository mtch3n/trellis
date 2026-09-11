# Component Registry

This file documents all UI components used in trellis and enforces the component policy defined in the design specification (§12.1 and §12.2).

## Policy

1. **shadcn/ui components are the default.** Before writing a custom component, search the registry at https://ui.shadcn.com and add the component from shadcn.

2. **Custom components require proof, recorded here before the component is written:**
   - The registry search actually run, and its result showing nothing suitable
   - Why composition of existing shadcn primitives cannot produce the behaviour
   - What the custom component owns that no primitive does
   - A link to the registry search result or archived screenshot

3. **Composition over custom markup.** Wrapping shadcn primitives to add behaviour is correct; reimplementing a primitive's markup to change appearance is not—restyle through Tailwind theme tokens instead.

4. **Two screens showing the same thing must show it with the same component.** Consistency is enforced by the `make ui-audit` script, not by instruction.

## Installed shadcn Components

| Component | Location | Purpose |
|-----------|----------|---------|
| Button | `@/components/ui/button` | Primary interactive element |
| Card | `@/components/ui/card` | Container for card layout (CardHeader, CardTitle, CardDescription, CardContent, CardFooter) |
| Input | `@/components/ui/input` | Single-line form control |
| Textarea | `@/components/ui/textarea` | Multiline Markdown source control |

## Wrapper Components

Components in `web/src/components/wrappers/` compose shadcn primitives and are listed here.

| Component | Wraps | Why |
|-----------|-------|-----|
| MarkdownContent | `react-markdown`, `remark-gfm`, `rehype-sanitize` | Safely renders stored Markdown with the shadcn Typeset stylesheet; sanitization prevents raw HTML from becoming an injection surface |

## Page Components

Page components handle routing and data fetching for each route.

| Component | Purpose |
|-----------|---------|
| ProjectsPage | Landing page showing all projects with per-column card counts |
| BoardPage | Board view showing cards organized by column |
| KnowledgePage | Markdown knowledge editor, label merge, and linked graph view |
| SearchPage | Cross-project card and knowledge search |
| ActivityPage | Global event feed |
| ProjectPage | Project-scoped board and knowledge navigation |
| CardPage | Deep-linked card detail, notes, and activity |
| ProjectKnowledgePage | Project and global knowledge index |

## Custom Components

`MarkdownContent` is the only custom wrapper. It owns Markdown parsing and
sanitization; visual typography remains in the shared shadcn Typeset stylesheet.

## Notes

- shadcn init installed Button with v4 Nova preset
- All imports must come from `@/components/ui` (shadcn registry) or `@/components/wrappers` (composed components)
- Raw HTML primitives (`<button>`, `<input>`, etc.) are forbidden where shadcn equivalents exist
- Ad-hoc colors, spacing, and radius values are forbidden—use Tailwind theme tokens
