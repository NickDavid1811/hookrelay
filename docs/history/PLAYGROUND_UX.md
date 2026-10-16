# Flow Lab UX review — 2026-10-14

[Español](../es/history/PLAYGROUND_UX.md) · Documentation review: 2026-10-16.

Historical review. Goal: a new reader should publish an example, explain HookRelay and distinguish acceptance from delivery without first learning SQLite, HMAC, backoff or internal states.

## Problems and applied changes

| Earlier issue | Effect | Applied correction |
| --- | --- | --- |
| 13 px base and many 7–11 px labels | Hard-to-read descriptions/history | 16 px base, 18 px main explanation, 14–16 px auxiliary text, 22–24 px section headings |
| Metrics first and three simultaneous columns | No obvious starting sequence | Three steps: publish, explore route, check result |
| Acronyms and a moving dot | Route shown without explanation | Six numbered selectable stages, examples and one key idea each |
| Text followed animation | Not enough reading time | Manual reading default, Previous/Next and optional following |
| Real resend mixed with drawing playback | Repeated drawing could look like another delivery | Explicit pause/repeat drawing labels and separate real state |
| Mobile SVG scrolled horizontally | Flow partly outside viewport | Vertical sequence; selected stage reveals explanation |
| JSON/IDs/metrics always visible | Technical overload | Ready example and collapsible advanced details |
| Technical scenario labels | Required prior implementation knowledge | Plain-language names and expected outcomes |
| Badge/counts dominated result | Receipt confused with business processing | Explicit success/wait/exhaustion and duplicate-effect warning |
| Small icon-only controls | Actions hard to discover/use | Visible labels, at least 44 px targets, focus and native buttons |

## Interaction criteria

Publishing starts actual work. Manual stage selection retains the explanation while the backend progresses. Animation is illustrative, never an exact transaction trace. The save stage explains 202 as durable acceptance, not receiver delivery. Failure explains waiting/sending/confirmation loops and five-attempt exhaustion. Attempt details and IDs remain available without dominating the screen. Only the history table may scroll horizontally on small screens. Reduced-motion behavior retains explanations and controls without relying only on movement/color.

## Recorded validation and limits

TypeScript/Vite build and five playback-model tests passed. Speed testing checked the 2× relationship without a fixed duration; the visual route slowed from approximately 2.5 to 8.4 seconds at 1×. All six process scenarios, recovery/replay and export passed. This UX pass changed no backend code.

Browser inspection covered stage selection/Next, a successful publication at 390×844, persisted result and no horizontal page overflow; desktop diagram at 1280×900. Inspected visible text started at 14 px. Lost-response behavior was inspected with drawing paused to distinguish real results from playback.

This was functional inspection, not user-comprehension research or WCAG certification. The suggested follow-up was a new user's unaided explanation of 202 vs 204, retries and duplicate effects, recording confusion before further refinement. These dated observations are not a fresh browser audit of future revisions. The current guide is [Flow Lab](../PLAYGROUND.md).
