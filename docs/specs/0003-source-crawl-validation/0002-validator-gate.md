# Docker validator feasibility gate

## Summary

Real validation uses a separate Linux arm64 container managed by local Docker. A disposable container is useful only when its remote registration can also be cleaned up. This file retains the experiment protocol; measured results and the implementation contract are now recorded in [the backend child](0003-docker-validator.md).

## Original prerequisites

You reported no prepared Linux environment and no controlled subscription with device slot visibility. Docker CLI `29.9.0`, build `f415da838b`, was observed locally on 2026-10-10. That observation establishes only the client binary. No Docker daemon, image, WARP container, registration, entitlement, or cleanup procedure was tested during this design.

Before any attachment, you need a functioning local Docker endpoint capable of Linux arm64 execution, an isolated image containing a supported WARP daemon and CLI, and a subscription you control with independently visible device membership. Record how you observe device membership before and after the experiment. Without that visibility, the cleanup part cannot pass. A publicly posted key cannot replace this control account for proving slot reclamation.

The application remains native on macOS. Docker Desktop normally runs containers inside its own Linux VM; the disposable unit here is the container, not a newly created VM per check. Another local Docker runtime may be tested, but its actual identity and versions must be recorded. A remote Docker endpoint is outside this decision and cannot be selected silently. (basis: your macOS and Docker choice, [Docker platform documentation](https://docs.docker.com/faqs/platform/))

The later authorized experiment supplied local Colima, a pinned official Linux client, matching Unlimited evidence, and independent membership inspection through a separate authorized registration. The user confirmed owner permission to activate the shared candidates. Process and container interruption checks subsequently exercised retained recovery and deliberate blocking on a lost deletion acknowledgement. The working host registration was not used as a validator and was not changed by the implementation. A fresh positive check through the final CLI remains pending by the user's choice on 2026-10-11.

## Experimental boundary

Begin with one controlled candidate and one container at a time. This is an experiment limit, not a production concurrency guarantee. Record the image digest and verify its actual Linux arm64 architecture instead of trusting a mutable image tag. Record the WARP CLI and daemon versions, Docker client and server versions, runtime identity, and host platform. Choose the base distribution and WARP package only after checking their available arm64 compatibility during provisioning; no compatible image has been established by this draft.

Use a dedicated container network and private container state. Do not share the host network, WARP directories, host device registration, or Docker socket with the container. Do not use `--privileged`. Begin with ordinary capabilities; if the observed daemon requires tunnel access, evaluate only the specific device and `NET_ADMIN` capability required. Docker documents these controls, but their sufficiency for WARP is unproven. If this boundary cannot support WARP, record failure and return to architecture rather than weakening isolation automatically. (basis: [Docker run controls](https://docs.docker.com/engine/containers/run/), [host network behavior](https://docs.docker.com/engine/network/drivers/host/))

The future transport would be local Docker CLI subprocesses, executed with structured argument arrays and no shell interpolation. During the experiment, prove the local endpoint and resource names being used. Never place the controlled key in the host command line, an environment variable, a container label, image layer, or repository file. Deliver it through private input to the isolated process if the tested CLI permits that securely. If the actual WARP CLI requires a key argument inside the container, document that exposure boundary and keep its process and output inaccessible outside the operator's controlled runtime. Do not invent an unsupported CLI option to hide the issue.

Do not use automatic `--rm`. Retain container state and recovery information until remote cleanup has been positively observed. Stopping a container to contain uncertain activity is permitted without deleting its state. Neither a stopped container nor a destroyed container proves that a remote subscription slot was released.

## Durable experiment record

Use an operator chosen absolute private laboratory directory outside the repository and separate from the application database. It is an experimental evidence artifact, not a new application table or a production recovery implementation. Its directory uses `0700` and files use `0600`. One experiment owner holds an advisory lock in that directory before creating or resuming a session. Until the unresolved session is reconciled, that laboratory must not start another attachment, even under a new container name.

Maintain a structured journal with atomic replacement and durable file and directory synchronization before remote changes. A failed journal write means no next remote action. Retain the previous durable state after failure. Do not store raw keys or registration credentials in the public report. Private credentials needed for reconciliation remain in controlled container state or a separate private artifact whose path is recorded in the journal.

| Required journal fact | Source |
| --- | --- |
| Experiment ID and planned unique container name | Locally generated random identity recorded before container creation |
| Local Docker endpoint and runtime identity | Inspected local Docker context and daemon, not an assumed default |
| Image digest, architecture, CLI and daemon versions | Image and process inspection in the isolated environment |
| Controlled account reference and fixture reference | Operator supplied private references, never a posted live key in the report |
| Candidate attribution | Private fingerprint of the exact controlled input and recorded isolated registration identity; the published report uses an opaque fixture label |
| Container ID and recovery state location | Docker creation and inspection results, recorded before registration work |
| Phase, intended next operation, and timestamps | Experiment owner and UTC clock, flushed before each remote mutation |
| Baseline and final device membership evidence | Independent account inspection under the controlled subscription |
| Normalized entitlement and observation time | Actual isolated response attributable to the submitted input |
| Cleanup result and unresolved reason | Observed response plus independent membership reconciliation |

Recommended phases are `prepared`, `registration_pending`, `registered`, `attachment_pending`, `attached`, `cleanup_pending`, and `cleaned`. Record the pending phase before the action; record success only after attributable evidence. If a crash or timeout occurs in a pending phase, resume by inspecting and reconciling the known resource before repeating any command. A timeout means uncertainty, not proof that an operation never reached the service.

The future real adapter will need an application owned recovery model tied to candidate and attempt IDs. This laboratory journal does not settle that schema. It is evidence for the backend design gate below. Existing store recovery alone cannot clear an unresolved remote session.

## Protocol and pass conditions

The experiment is a sequence of measurements. Actual mutation commands must come from the installed isolated CLI and supported behavior, recorded before execution. This document does not supply guessed attachment or deletion flags.

1. Provision the runtime without touching the host WARP client. Verify separate registration storage, network configuration, and routing. Record baseline host connectivity and routing using passive operating system observations. Establish how to detect any change during the test. A separate directory alone is insufficient.
2. Establish the controlled account baseline, including available device membership. Record the exact entitlement evidence expected from that account. Unknown baseline state blocks attachment.
3. Create and inspect a uniquely named isolated container and its fresh registration, journaling intent and results. Prove that operations address this registration and cannot fall back to the host daemon.
4. Apply the controlled candidate to that isolated registration. Read back enough account and registration identity to attribute the result to the submitted candidate. Command success or ordinary WARP connectivity is not proof. The initial accepted positive evidence remains exact account type `Unlimited`, as specified in spec 0001.
5. Exercise normal cleanup and independently verify that the temporary device no longer consumes a subscription slot. Record the tested method, account view, before and after membership, elapsed time, and any reconciliation delay. Local deletion or inability to reconnect is insufficient evidence.
6. Exercise interruption after attachment intent, after a confirmed attachment, and during cleanup. Include a timeout with an ambiguous remote result and a process termination. Resume from the durable journal and prove no new attachment occurs while cleanup is unresolved. Do not create an uncontrolled retry loop to obtain this proof.
7. Verify container removal only after the journal records proven cleanup. Recheck host connectivity and routing throughout the experiment and after disposal. Retain a redacted report and private evidence until the backend design is reviewed.
8. Measure cold start cost, memory, CPU, request count, and observed operation duration at concurrency one. A provisional individual operation deadline of 60 seconds and total active experiment deadline of five minutes bound the initial run. Expiry records uncertainty, stops new mutation, and retains state for reconciliation. These are experimental limits, not claims about sufficient production timing or cleanup convergence.

Do not intentionally damage or attach to other people's subscriptions to obtain negative controls. Use a controlled free registration or clearly synthetic invalid input when appropriate, and distinguish what that control actually proves. Only a known definitive provider rejection may become `invalid`. Device limits, rate limits, unknown account forms, transport errors, and unclassified output remain inconclusive. If no definitive rejection semantics are proven, the eventual backend may support confirmation and inconclusive outcomes only.

Every condition in parent AC-7 must pass. An untested condition is pending; a failed condition is failed. Both leave the backend unsupported. A failed container boundary may justify a separate VM design later, but does not authorize using the host registration.

## Backend design gate

After successful measurements, `/architect` adds the concrete backend child to this umbrella and updates its cross child contract before `/develop` implements real validation. That child must contain:

1. Exact supported runtime, image digest policy, packages, commands, privileges, local transport, and version compatibility checks.
2. A tested source for candidate to registration attribution and normalized evidence, plus proven rejection codes if any.
3. The production session and recovery schema, resource names, secret lifecycle, writer ownership, and behavior across application profiles and Docker daemon restart.
4. Atomic ordering of durable intent, attachment, evidence persistence, cleanup, and container disposal, including uncertain store writes and a cleanup failure after successful entitlement observation.
5. Measured deadlines, cleanup convergence bounds, reconciliation rules, and the fail closed attachment gate.
6. Capability reporting, accepted configuration, exact CLI behavior when Docker or required versions are absent, and the supported platform matrix.
7. Verification using both controlled positive evidence and a real sourced candidate, with no promise that the public source currently yields a confirmable key.

This gate is deliberately not marked complete by the presence of a Docker binary, a working container, a successful ordinary connection, or a persisted fixture. Parent AC-8 stays unmet until the proven backend performs the actual source to check to saved read path.

## Rationale

Docker keeps the user facing process on macOS and supplies the chosen Linux experiment boundary. A dedicated Linux VM is the alternative if containers cannot meet isolation requirements. Retaining state on uncertainty trades automatic disposal for recoverability; remote cleanup and local lifecycle are separate facts. The controlled account requirement prevents drawing conclusions from device slots that cannot be observed. (basis: your confirmed choices, spec 0001's feasibility requirements, Docker documentation cited above)
