# Audit pack: continuity

Read by the audit workflow prompt (`vaquero workflow audit --project KEY --kind continuity`). Save it in the project's repository as `.vaquero/audits/continuity.md` and fill in each section. Keep the headings as they are: the prompt finds each section by its heading.

## Standards

<Where the written UI and copy rules live, and the shared component definitions to read.>

## Dimensions

A deviation breaks a written rule, quoted here. A discontinuity breaks no rule but leaves one surface unlike its peers; its method is below.

| # | Kind | Dimension | Checks against |
|---|---|---|---|
| 1 | deviation | Theming and tokens | <the quoted rule> |
| 2 | deviation | Copy and voice | <the quoted rule> |
| 3 | deviation | Accessibility and interaction | <the quoted rule> |
| 4 | deviation | Role gating | <the quoted rule> |
| 5 | discontinuity | Partial adoption of shared components | Methods, 5 |
| 6 | deviation | Narrow viewport (a suspicion from source) | Methods, 6 |
| 7 | discontinuity | Peer check across surfaces | Methods, 7 |

## Methods

### 5. Partial adoption of shared components

<Each shared component, and the hand-rolled equivalent to count against it.>

### 6. Narrow viewport

<The grep that finds rows likely to break when narrow, and the questions that confirm one.>

### 7. Peer check across surfaces

<The peer groups: which surfaces are structural siblings.>

## Search surface

<View sources, helpers, handlers, client scripts and CSS. Never generated output.>

## Known non-findings

- <A deliberate exception, including a peer difference that is correct, and why.>
