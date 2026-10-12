# Source crawl and validation reasoning

## Context

> Premise note: A disposable Linux container does not make a WARP subscription attachment disposable. Without observed remote cleanup, discarding local state can leave an occupied device slot and remove information needed to reconcile it.

You want the first real crawl, selected check, persistence, and read path. The CLI currently has only help and version. The existing store already separates source provenance, execution history, attempts, definitive evidence, and freshness. Scope item 4 is planned under Tracer Bullet delivery and the Beta workflow. (basis: [scope](../../scope/scope.md), [root instructions](../../../AGENTS.md), [store contract](../../../internal/store/AGENTS.md))

Your fixed choices for this feature are one latest public page from `warpplus`, deliberate checking by saved candidate ID, saved coverage including posts without candidates, explicit unsupported attempt history, text output, and hidden keys unless requested. You selected goquery, a 20 second source request limit, a 2 MiB body limit, and explicit failure for unrecognized markup. The feature adds neither historical crawling nor scheduled, parallel, or automatic checking. (basis: design conversation)

You chose macOS as the first user facing runtime and Docker for an isolated Linux arm64 experiment. You have no prepared Linux validator environment and no controlled subscription with device slot visibility. Those prerequisites are unsatisfied. The existing architecture remains authoritative about entitlement, isolation, and cleanup; choosing Docker refines the experiment environment and does not weaken that gate. (basis: [spec 0001](../0001-stack-architecture/index.md), design conversation)

## Options considered

| Option | Benefit | Cost | Outcome |
| --- | --- | --- | --- |
| Collect and list only, with no attempt history | Smallest useful path and no registration dependency | Does not exercise the validation boundary or retain an explicit check request | Rejected in favor of recording unsupported attempts. (basis: your selected check behavior) |
| Native macOS CLI, one public page, existing store, explicit unsupported adapter, gated Docker experiment | Proves the application path while keeping its missing external evidence visible | Full completion remains blocked and a concrete backend contract must follow measurements | Selected. (basis: scope item 4, your Docker and macOS preference, spec 0001) |
| Run the whole application in a dedicated Linux VM | Fewer host to guest transport assumptions for a possible Linux backend | Changes the requested macOS workflow and still needs remote cleanup proof | Retained as a possible later response to a failed container experiment, not selected now. (basis: design conversation, separation of local isolation and remote cleanup) |
| macOS CLI with a remote Linux validator service | Can reuse an existing separate Linux host | Adds transport, authentication, service lifecycle, and another trust boundary before a backend is proven | Deferred. (basis: spec 0001, no remote environment supplied) |

## Rationale

The existing store is the natural integration point because it already owns identity, durable histories, freshness, and recovery. A coverage row adds the source facts the user asked to inspect after restart, including attempts that never produced a parsed page. Derived saved counts keep the report consistent with actual committed observations. Keeping separate manual commands makes candidate selection explicit and prevents a source fetch from attaching subscriptions. (basis: [spec 0002](../0002-persistent-key-state/index.md), confirmed data model, single writer ownership)

goquery reduces custom tree traversal while preserving the HTML5 parser underneath. Direct `golang.org/x/net/html` remains a sound alternative when selector convenience does not justify another dependency. The user chose goquery in line with the project's preference for libraries that remove meaningful complexity. No new skill or MCP discovery was performed because it had already been declined. (basis: [goquery](https://github.com/PuerkitoBio/goquery), [HTML parser](https://pkg.go.dev/golang.org/x/net/html), root instructions)

One fixed URL and one bounded response limit the initial source contract. Strict scoped extraction preserves provenance and avoids treating arbitrary links or page chrome as key messages. The public preview remains an external HTML surface, so fixtures and an observed live parse must accompany implementation. The current source can yield no usable candidates, and the design does not promise a live quantity. (basis: [public preview](https://t.me/s/warpplus), bounded input processing)

Docker is an experiment choice, not established validator support. Its Linux VM and container controls can provide a test boundary, while its removal options govern local resources only. The lab must prove the narrower permissions suffice and that cleanup can be independently observed. Until then, an unsupported result is the only truthful real application outcome. (basis: [Docker platform](https://docs.docker.com/faqs/platform/), [run controls](https://docs.docker.com/engine/containers/run/), [host networking](https://docs.docker.com/engine/network/drivers/host/), spec 0001)

## Research evidence and limits

On 2026-10-10, a read only research helper confirmed that the public `warpplus` preview was reachable and contained posts with candidate shaped strings, timestamps, and other channel content. It observed three groups of eight ASCII letters or digits separated by hyphens. It did not inspect a raw DOM fixture or execute a parser. No live key values were included in this record. (basis: public preview observation)

The research verified the goquery repository and `x/net/html` package documentation. goquery documentation described a Go 1.25 minimum for its v1.12 line, compatible with the repository's observed Go 1.27.2 toolchain. This is not a tested dependency selection; implementation must record the exact version it builds and verifies. (basis: goquery project, repository `go.mod`)

The research found no primary evidence establishing that consumer registration deletion frees a subscription device slot. A Cloudflare One client changelog discussed registration deletion in a different product context; it does not establish consumer cleanup semantics. The existing architecture's Linux `Unlimited` target remains the starting hypothesis for controlled testing. The search finding is bounded uncertainty, not proof that cleanup is impossible. (basis: spec 0001's research record, [Cloudflare One client changelog](https://developers.cloudflare.com/changelog/product/cloudflare-one-client/))

Docker documentation distinguishes containers from the Linux VM on macOS and describes specific capability and device grants. It does not prove WARP works in that environment. The local `docker --version` command returned `29.9.0`, build `f415da838b`; no daemon or WARP command was run. (basis: Docker documentation, local command observation)

Repository preflight found a clean local `main`. Fetch succeeded, but no `origin/main` reference was available for a behind count. No commit, push, or application change was made during design. Existing specs 0001 and 0002 remain separate accepted contracts. (basis: local Git inspection)

## Independent cross check

On 2026-10-10, a separate model reviewed all four draft files and the relevant existing contracts without editing files, fetching references, running tests, or invoking Docker or WARP. It found no substantive decision gaps or soundness issues in the initial crawl, coverage, unsupported check, configuration, and listing path. It recognized the missing experiment prerequisites, live DOM verification, and later real backend contract as explicit gates rather than hidden implementation choices. This review is not runtime verification and does not establish real validator support.

You accepted the reviewed spec on 2026-10-10. Scope item 4 now links this decision and its build milestones. The feature spec remains `Proposed` because implementation has not begun; the full feature cannot complete until the experimental and real backend criteria pass.

## References

**Project sources**: [Scope](../../scope/scope.md), [AGENTS.md](../../../AGENTS.md), [store instructions](../../../internal/store/AGENTS.md), [architecture](../0001-stack-architecture/index.md), [persistent state](../0002-persistent-key-state/index.md), design conversation, and local Docker and Git observations.

**Practices**: Tracer Bullet delivery, single writer ownership, durable intent before external mutation, explicit uncertainty, bounded input processing, and separate local and remote resource lifecycles.

**Verified links**:

1. [Public warpplus preview](https://t.me/s/warpplus), observed source content rather than a stable crawler API.
2. [goquery project](https://github.com/PuerkitoBio/goquery), selected parsing library.
3. [Go HTML parser](https://pkg.go.dev/golang.org/x/net/html), parser underneath goquery and the direct traversal alternative.
4. [Docker platform documentation](https://docs.docker.com/faqs/platform/), Linux VM and host platform distinction.
5. [Docker run controls](https://docs.docker.com/engine/containers/run/), explicit capabilities, devices, and local lifecycle controls.
6. [Docker host networking](https://docs.docker.com/engine/network/drivers/host/), boundary intentionally excluded from the experiment.
7. [Cloudflare One client changelog](https://developers.cloudflare.com/changelog/product/cloudflare-one-client/), different product context that does not prove consumer slot reclamation.

These links record the design research for human reference. Later spec reviews and builds can use the recorded findings without fetching the pages again.

## Linux scope amendment, 2026-10-12

The macOS and arm64 choices above record the original experiment. Current supported operation is Linux amd64 and arm64 under spec 0001. The backend child defines one immutable image per architecture, matching native executable and daemon admission, and unchanged shared recovery. All earlier measured arm64 results remain historical evidence; amd64 entitlement, cleanup, and failure handling need their own runtime proof before support is claimed. macOS verification is no longer required.
