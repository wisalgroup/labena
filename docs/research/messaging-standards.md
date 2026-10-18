# Primary-source criteria for queue and pub/sub architecture

Researched 2026-09-29 for the comparison of `go-modular` and `freight`. This note establishes review criteria; it does not inspect or rate either implementation. Sources are official Go, broker, or library documentation. Broker features must be checked against the versions and configuration used by each project; current RabbitMQ pages describe 4.3.

## 1. Separate distribution semantics from durability

- **Competing consumers and fanout are different behaviors.** A NATS queue group receives one copy shared by its members; a plain subscriber receives its own copy. Distinct group names create independent logical subscribers. RabbitMQ fanout exchanges copy messages to every bound destination. Consequently, scaling replicas of one logical subscriber should generally preserve its group/queue identity, while separate business reactions need independent delivery state. The last sentence is a design inference, not a required Go package layout. Sources: [NATS queue groups](https://docs.nats.io/learn/core-nats/queue-groups), [RabbitMQ exchanges](https://www.rabbitmq.com/docs/exchanges).
- **A queue-shaped API does not imply persistence.** Core NATS is at-most-once, without storage or acknowledgements; an offline subscriber misses publication. Redis Pub/Sub is also at-most-once and cannot recover a message lost through disconnection or handling failure. These are valid ephemeral transports when the application tolerates loss. Sources: [Core NATS](https://docs.nats.io/learn/core-nats/), [Redis Pub/Sub delivery semantics](https://redis.io/docs/latest/develop/pubsub/).
- **Streams and consumer groups add different capabilities.** Redis Streams retain entries and support consumer groups with acknowledgement, pending-item inspection, and claiming abandoned work; separate groups can consume the same stream independently. Source: [Redis Streams](https://redis.io/docs/latest/develop/data-types/streams/).

Review implication: assess whether the contract documents fanout, competition, startup/offline behavior, replay, retention, and loss separately. Do not award a system durability merely because it has `Publish`, `Subscribe`, or a driver interface.

## 2. Define exactly what successful publication and handling mean

- **Publisher confirmation and consumer acknowledgement are independent.** RabbitMQ confirms cover acceptance by the broker/queue, not completion by an application. An unroutable publication can still receive a confirm; mandatory publication and returned-message handling address routing failure. Durable queue metadata alone also does not guarantee persistent message delivery. Sources: [RabbitMQ acknowledgements and confirms](https://www.rabbitmq.com/docs/confirms), [queue durability](https://www.rabbitmq.com/docs/queues#durability).
- **Acknowledge after the required work is complete.** RabbitMQ describes acknowledgement as transfer of responsibility: consumers should finish the necessary persistence, forwarding, or other operation first. Unacknowledged work can return after failure. If a publisher retries after losing a confirmation, the original publication may already exist, producing duplicates. Source: [RabbitMQ reliability guide](https://www.rabbitmq.com/docs/reliability).

Review implication: follow the precise return/error boundary from application to driver. Verify handling of broker rejection, routing failure, lost acknowledgements, and errors that leave acceptance uncertain. A `nil` return needs an explicit meaning. Successful publication is not evidence that every subscriber completed its business operation.

## 3. Design duplicate-safe effects and database/publication consistency

- **At-least-once delivery needs idempotent handling.** RabbitMQ explicitly recommends deduplication or idempotent consumers for retransmissions and redeliveries. The application must decide which business operation repeated delivery represents. Source: [RabbitMQ reliability guide](https://www.rabbitmq.com/docs/reliability).
- **An outbox addresses the dual-write gap.** AWS describes saving business state and an outbox record within the same database transaction, then relaying committed records. Rollback must not emit an event; relay duplicates still require idempotent consumers. Source: [AWS transactional outbox guidance](https://docs.aws.amazon.com/en_en/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html).
- **Direct transactional database publication is a legitimate alternative.** Watermill documents publishing through the same database transaction as application state, optionally forwarding to another broker afterward. A separate outbox relay is useful when the final broker is outside that transaction. Source: [Watermill Forwarder](https://watermill.io/advanced/forwarder/).

Review implication: inspect the atomicity boundary, not just the presence of an `Outbox` type. Deduplication records must protect the relevant effect; a completed external effect followed by a failed local marker can still be repeated. A transport deduplication feature or lease does not by itself prove exactly-once external business effects. These are failure-scenario deductions from the documented independent publication, effect, and acknowledgement boundaries.

## 4. Bound retries and preserve failed work deliberately

- **Immediate requeue can create a hot loop.** RabbitMQ warns that consumers repeatedly requeueing transient failures consume bandwidth and CPU, and describes delayed retry or final rejection as alternatives. Source: [RabbitMQ requeue behavior](https://www.rabbitmq.com/docs/confirms#negative-acknowledgement-and-requeuing-of-deliveries).
- **An attempt limit alone is not a dead-letter workflow.** JetStream uses `AckWait`, retry backoff, `MaxDeliver`, and terminal acknowledgement for different purposes. Exhausting delivery attempts stops delivery to that consumer while the record remains in the stream; applications can observe an advisory and implement recovery. An acknowledgement deadline shorter than work duration can cause simultaneous handling unless progress acknowledgements or a suitable deadline are used. Source: [NATS acknowledgements and redelivery](https://docs.nats.io/learn/jetstream/acknowledgment).
- **Dead-letter transfer has its own reliability boundary.** RabbitMQ's default dead-letter republishing omits internal publisher confirms and can lose messages when the target cannot accept them. Quorum queues can provide at-least-once dead-lettering when specifically configured. Sources: [RabbitMQ dead-letter safety](https://www.rabbitmq.com/docs/dlx#safety), [quorum queue dead-lettering](https://www.rabbitmq.com/docs/quorum-queues).

Review implication: distinguish transient, permanent, cancellation, and unknown failures; document attempt counting, delay, poison-message retention, diagnostics, and replay. Test failure of the retry/dead-letter write itself. Confirm whether a handler timeout really ends execution or merely makes another worker eligible to execute it.

## 5. Backpressure and ordering are part of the contract

- **Bound work in flight.** RabbitMQ prefetch limits outstanding unacknowledged deliveries; zero is unlimited, with potential memory growth. JetStream `MaxAckPending` limits outstanding work across its consumer, including all attached workers. Sources: [RabbitMQ prefetch](https://www.rabbitmq.com/docs/confirms#channel-prefetch-setting-qos), [NATS pull consumers](https://docs.nats.io/learn/jetstream/pull-consumers).
- **FIFO storage is not a blanket processing-order promise.** RabbitMQ documents interleaving across publishing channels and order changes from priorities, multiple active consumers, and redelivery. Its options include a single active consumer or partitioning by entity for ordered concurrent workloads. Source: [RabbitMQ message ordering](https://www.rabbitmq.com/docs/queues#message-ordering).

Review implication: inspect worker count, fetch size, channel capacity, payload limits, retention limits, and overload policy together. Check whether one slow subscriber can stall unrelated subscribers. Require an explicit scope for any ordering guarantee, such as within one entity or partition. Performance rankings require representative measurements rather than counting goroutines or supported drivers.

## 6. Go lifecycle and API design

- **Cancellation is a signal, not a join.** The Go `context` contract states that calling a cancel function does not wait for work to stop. A worker must cooperate with cancellation, and its owner needs another means of awaiting completion. Source: [Go context](https://pkg.go.dev/context#CancelFunc).
- **Goroutines need an exit path.** The Go pipeline guidance shows how blocked sends leak goroutines when downstream consumers stop; buffered channels only absorb a bounded number of values. Cancellation paths and completion coordination solve different parts of the problem. Source: [Go pipelines and cancellation](https://go.dev/blog/pipelines).
- **Prefer necessary, consumer-oriented abstractions.** Go Code Review Comments advises defining interfaces where they are consumed, avoiding speculative interfaces solely for mocks, making goroutine lifetimes clear, and generally preferring synchronous APIs whose callers can add concurrency. Source: [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments).

Review implication: trace construction, registration, start, readiness, shutdown, and resource ownership explicitly. Test stop-during-publish, stop-during-handler, startup failure, repeated close, and borrowed-resource handling. Prefer the smallest API that expresses the real guarantees. These sources do not prescribe one universal architecture, directory tree, number of layers, or framework.

## Suggested comparison evidence

Treat the following as reviewer-derived checks, not claims that a broker guarantees them automatically:

1. Two replicas of one subscriber share work; two different logical subscribers receive independent copies.
2. Offline subscribers, restarts, and retention expiry behave as documented.
3. Crash before the effect, after the effect but before acknowledgement, and after broker acceptance but before publisher confirmation have deliberate outcomes.
4. Database rollback emits nothing; commit plus failed relay remains recoverable.
5. A malformed or permanently failing message cannot retry forever without an explicit policy.
6. Overload remains bounded and visible; shutdown stops intake and waits for owned workers within a documented budget.
7. Real-backend tests exercise leases, acknowledgements, persistence, reconnect, and duplicate behavior. In-memory tests alone establish only the in-memory implementation's semantics.

Use those observations to compare maintainability and correctness separately from feature breadth. A simpler ephemeral implementation can be appropriate for its declared use case; it is not an equivalent substitute for a durable application workflow.
