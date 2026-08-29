import Link from 'next/link'

const GITHUB = 'https://github.com/Rakshit-gen/inferoute'

function H2({ id, children }: { id: string; children: React.ReactNode }) {
  return (
    <h2 id={id} className="mt-14 scroll-mt-24 font-display text-xl font-semibold tracking-tight">
      {children}
    </h2>
  )
}

function H3({ children }: { children: React.ReactNode }) {
  return <h3 className="mt-8 font-display text-base font-semibold tracking-tight text-ink">{children}</h3>
}

function P({ children }: { children: React.ReactNode }) {
  return <p className="mt-3 text-sm leading-relaxed text-ink-dim">{children}</p>
}

function Code({ children }: { children: React.ReactNode }) {
  return (
    <code className="rounded bg-panel-2 px-1.5 py-0.5 font-mono text-[0.85em] text-ink">{children}</code>
  )
}

function Pre({ children }: { children: string }) {
  return (
    <pre className="panel mt-3 overflow-x-auto p-4 font-mono text-xs leading-relaxed text-ink-dim">
      {children}
    </pre>
  )
}

function DefList({ items }: { items: [string, React.ReactNode][] }) {
  return (
    <dl className="mt-4 space-y-3">
      {items.map(([term, desc]) => (
        <div key={term} className="panel p-3">
          <dt className="font-mono text-sm text-ink">{term}</dt>
          <dd className="mt-1 text-sm leading-relaxed text-ink-dim">{desc}</dd>
        </div>
      ))}
    </dl>
  )
}

const toc: [string, string][] = [
  ['what-is-it', 'What inferoute is'],
  ['the-problem', 'The problem it solves'],
  ['lifecycle', 'What happens to a request'],
  ['concepts', 'Concepts and vocabulary'],
  ['getting-started', 'Getting started'],
  ['config', 'Configuration reference'],
  ['api', 'HTTP API reference'],
  ['headers', 'Response headers'],
  ['reload', 'Changing config without downtime'],
  ['rate-limiting', 'Rate limiting in depth'],
  ['caching', 'Semantic caching in depth'],
  ['metrics', 'Metrics and observability'],
  ['deploying', 'Deploying to production'],
  ['dashboard', 'Using this dashboard'],
  ['limits', 'Current limitations'],
]

const concepts: [string, React.ReactNode][] = [
  [
    'Backend',
    'One inference server inferoute forwards requests to: an Ollama process, a vLLM server, a hosted provider like OpenAI or Groq, anything that speaks the OpenAI chat-completions API. You list your backends in the config file.',
  ],
  [
    'Model',
    'The name a caller puts in the "model" field of the request body, for example "llama3". Each backend declares which models it can serve. inferoute uses that field to decide where a request can go.',
  ],
  [
    'Model alias',
    'A rename. If your callers expect "gpt-4" but your servers actually run "llama3:70b", an alias ({"gpt-4": "llama3:70b"}) lets the request come in as "gpt-4" and get routed as "llama3:70b". Nothing about the caller changes.',
  ],
  [
    'Routing',
    'Choosing which backend handles a given request. inferoute looks at the model, filters to the backends that serve it and are currently healthy, and picks one.',
  ],
  [
    'Round-robin',
    'The selection rule: requests for the same model are spread evenly across that model\'s healthy backends, one after another, so no single server takes all the load.',
  ],
  [
    'Failover',
    'If the chosen backend refuses the connection or returns a server error, inferoute marks it unhealthy and immediately retries the request on the next healthy backend, up to three attempts, before giving up.',
  ],
  [
    'Health check',
    'A background loop that sends a plain GET to each backend on a path and interval you configure. A backend that stops responding is taken out of rotation until it recovers, so failover usually happens before a caller ever sees an error.',
  ],
  [
    'Rate limit (token bucket)',
    'A cap on how fast one caller can send requests. Each API key gets a bucket that refills at a steady rate (requests_per_second) and holds a small reserve (burst). A request takes one token; when the bucket is empty the caller gets HTTP 429 until it refills.',
  ],
  [
    'Semantic cache',
    'An optional response cache keyed on the meaning of the prompt rather than an exact string match. Two prompts that are close enough in an embedding vector space are treated as the same question, so the second one is answered instantly from storage instead of hitting a backend.',
  ],
  [
    'Embedding distance',
    'A number measuring how far apart two prompts are in meaning. 0 is identical, larger is less similar. The cache counts a stored prompt as a match when its distance to the new prompt is below max_distance. Smaller max_distance means stricter matching.',
  ],
]

const configFields: [string, React.ReactNode][] = [
  ['listen_addr', 'The address inferoute itself listens on. Default ":8081".'],
  [
    'health_check_path, health_check_interval',
    'The path to GET on every backend to confirm it is alive, and how often to do it. Defaults: "/" every 10 seconds.',
  ],
  [
    'backends (required)',
    'The list of servers to route to. Each entry is { name, url, models, path_prefix?, api_key? }. "models" is the list of model names that server can handle; two backends listing the same model get load-balanced between. "path_prefix" is prepended to the forwarded path for servers that mount their API under a prefix (Groq serves its OpenAI-compatible API under "/openai", so path_prefix ":/openai" turns "/v1/chat/completions" into "/openai/v1/chat/completions"). "api_key" is sent as that backend\'s Authorization header, overriding whatever the caller sent, so you can put a paid provider behind inferoute without callers knowing the key.',
  ],
  ['rate_limit.enabled', 'Turn per-key rate limiting on or off. Off by default.'],
  [
    'rate_limit.requests_per_second, .burst',
    'The steady refill rate and the reserve size. A caller can spend up to "burst" requests quickly, then is held to "requests_per_second" after that.',
  ],
  [
    'rate_limit.redis_addr',
    'Leave empty and each inferoute process keeps its own count (fine for a single instance). Set it to a Redis "host:port" and several inferoute instances behind a load balancer share one limit.',
  ],
  [
    'cache.enabled',
    'Turn semantic response caching on or off. Off by default. Requires a running NuclaDB instance.',
  ],
  ['cache.nucladb_addr', 'Where that NuclaDB instance is reachable.'],
  [
    'cache.embedding_backend_addr, .embedding_model',
    'Which Ollama-compatible server and model turn a prompt into a vector for cache lookups.',
  ],
  [
    'cache.max_distance',
    'How close a stored prompt must be to count as a hit. This is a distance, not a similarity score: 0 means identical, and a smaller number is a stricter match. See the caching section below, this one is easy to set backwards.',
  ],
  ['cache.tenant_id', 'The NuclaDB tenant the cache vectors are stored under.'],
  [
    'model_aliases',
    'Maps a requested model name onto one your backends actually serve, for example {"gpt-4": "llama3"}. Empty by default (no aliasing).',
  ],
  [
    'cors_origins',
    'Browser origins allowed to call the HTTP API directly. ["*"] allows any origin and is the default. Non-browser clients send no Origin header and are never affected.',
  ],
  [
    'api_keys',
    'An allowlist of bearer tokens for POST /v1/chat/completions. When the list is non-empty, a request without a listed "Authorization: Bearer <key>" gets HTTP 401. Empty (the default) leaves the proxy open. Reloaded on SIGHUP. The read-only endpoints are not gated by this.',
  ],
]

const endpoints: [string, React.ReactNode][] = [
  [
    'POST /v1/chat/completions',
    'The proxy itself. Reads "model" from the JSON body, resolves aliases, checks the cache when enabled, picks a healthy backend, and streams the response straight back. Returns 400 if the body is not JSON with a "model" field, 429 if rate limited, 401 if an API key is required and missing, 503 if no backend serves that model, 502 if every attempt failed.',
  ],
  [
    'GET /v1/models',
    'An OpenAI-compatible model list: every model a backend serves plus every alias clients can ask for. SDKs call this to fill a model picker. Not gated by api_keys.',
  ],
  ['GET /v1/backends', 'A JSON array of every backend with its name, url, models, and current health.'],
  [
    'GET /v1/config',
    'The active model aliases, the health-check interval, and whether rate limiting, caching, and API-key auth are on. This is what the dashboard reads to describe your gateway.',
  ],
  [
    'GET /metrics',
    'Prometheus text format: request counts and latency by model and backend, and cache hit / miss / error counts.',
  ],
  ['GET /healthz', 'A liveness probe for load balancers and orchestrators. Always 200 while the process is running.'],
  ['GET /docs', "inferoute's own documentation page, served straight from the binary."],
]

const headers: [string, React.ReactNode][] = [
  [
    'X-Inferoute-Backend',
    'The name of the backend that served the response, or "cache" when the answer came from the semantic cache.',
  ],
  ['X-Inferoute-Cache', 'Set to "hit" when the response was served from the cache. Absent otherwise.'],
]

export default function Docs() {
  return (
    <article className="max-w-3xl">
      <p className="eyebrow">reference</p>
      <h1 className="mt-3 font-display text-3xl font-bold tracking-tight">Documentation</h1>
      <P>
        This page explains what inferoute does, the ideas behind it, and every knob it exposes. No
        prior context needed. The source and issue tracker live on{' '}
        <a href={GITHUB} className="text-route hover:underline">
          GitHub
        </a>
        .
      </P>

      <nav className="panel mt-6 p-4">
        <div className="eyebrow mb-2">on this page</div>
        <ol className="grid gap-1 text-sm sm:grid-cols-2">
          {toc.map(([id, label], i) => (
            <li key={id}>
              <a href={`#${id}`} className="text-ink-dim hover:text-route">
                <span className="mono-num mr-2 text-xs">{String(i + 1).padStart(2, '0')}</span>
                {label}
              </a>
            </li>
          ))}
        </ol>
      </nav>

      <H2 id="what-is-it">What inferoute is</H2>
      <P>
        inferoute is a gateway that sits in front of one or more large language model servers and
        makes them look like a single, reliable endpoint. Your application talks to inferoute using
        the ordinary OpenAI chat-completions API. inferoute decides which server actually handles each
        request, retries elsewhere when one fails, enforces per-caller rate limits, and can answer
        repeat questions from a cache without touching a model at all.
      </P>
      <P>
        It is one small Go binary (<Code>inferouted</Code>) with one JSON config file. It stores
        nothing on disk, holds no database of its own, and adds a few milliseconds to a request. You
        run it next to your inference servers and point your clients at it instead of at them.
      </P>

      <H2 id="the-problem">The problem it solves</H2>
      <P>
        Running a language model is now the easy part: vLLM, SGLang, llama.cpp, and Ollama all do it
        well. The awkward part is everything around the model once more than one team, or more than
        one GPU, is involved:
      </P>
      <ul className="mt-3 space-y-2 text-sm text-ink-dim">
        <li>
          <span className="text-ink">One address for many servers.</span> You have three Ollama boxes
          serving the same model and want traffic spread across them without every client hard-coding
          three URLs.
        </li>
        <li>
          <span className="text-ink">Staying up when one dies.</span> A GPU node falls over at 3am and
          requests should quietly move to the survivors, not start failing.
        </li>
        <li>
          <span className="text-ink">Fair use.</span> One noisy script should not be able to starve
          everyone else of capacity.
        </li>
        <li>
          <span className="text-ink">Not paying twice for the same answer.</span> Support macros, test
          suites, and retried prompts ask the same thing repeatedly. Serving those from a cache is far
          cheaper than re-running inference.
        </li>
        <li>
          <span className="text-ink">Knowing what is going on.</span> Request volume, latency, and
          cache effectiveness per model and per backend, in a format Prometheus already understands.
        </li>
      </ul>
      <P>
        inferoute is that layer, and only that layer. It does not run models, fine-tune them, or
        store your conversations.
      </P>

      <H2 id="lifecycle">What happens to a request</H2>
      <P>
        Every call to <Code>POST /v1/chat/completions</Code> goes through the same sequence. This is
        the actual order of operations in the code, not a simplification.
      </P>
      <ol className="mt-4 space-y-4">
        {[
          [
            'Parse and identify',
            'inferoute reads the JSON body and pulls out the "model" field. A body that is not valid JSON, or has no "model", is rejected with HTTP 400 before anything else runs. If the name matches a model alias, it is swapped for the real name here.',
          ],
          [
            'API-key check',
            'If you configured an api_keys allowlist, the "Authorization: Bearer <key>" header must carry one of those keys. Otherwise the request gets HTTP 401. If the list is empty, this step does nothing.',
          ],
          [
            'Rate limit',
            'If rate limiting is on, the caller\'s token bucket (keyed on their API key, or their IP address if there is no key) must have a token available. If not, the caller gets HTTP 429 and the request stops here.',
          ],
          [
            'Cache lookup',
            'If caching is on, the prompt is turned into a vector and looked up in NuclaDB. If a stored prompt is within max_distance, its saved response is returned immediately, a streamed answer is replayed byte for byte, and no backend is contacted. The response carries X-Inferoute-Cache: hit.',
          ],
          [
            'Pick a backend',
            'On a cache miss, inferoute takes the healthy backends that serve this model and picks the next one in round-robin order. If none are healthy or none serve the model, the caller gets HTTP 503.',
          ],
          [
            'Forward and, if needed, fail over',
            'The request is forwarded to the chosen backend. On a connection error or a 5xx, that backend is marked unhealthy and the request is retried on the next one, up to three attempts total. If all three fail, the caller gets HTTP 502.',
          ],
          [
            'Stream back and record',
            'The backend\'s response is written to the caller as it arrives, never buffered. The response gets X-Inferoute-Backend naming the server that handled it. A successful, non-streamed answer to a cacheable request is stored in the cache in the background. Counters and the latency histogram are updated.',
          ],
        ].map(([title, body], i) => (
          <li key={title} className="flex gap-4">
            <span className="mono-num shrink-0 text-sm text-ink-dim">{String(i + 1).padStart(2, '0')}</span>
            <div className="border-l border-line pl-4">
              <div className="font-display text-sm font-semibold text-ink">{title}</div>
              <p className="mt-1 text-sm leading-relaxed text-ink-dim">{body}</p>
            </div>
          </li>
        ))}
      </ol>

      <H2 id="concepts">Concepts and vocabulary</H2>
      <P>Terms this documentation uses, defined once.</P>
      <DefList items={concepts} />

      <H2 id="getting-started">Getting started</H2>
      <P>
        The goal of this walk-through is a running gateway in front of two model servers, with one
        request going through it.
      </P>

      <H3>1. Have something to route to</H3>
      <P>
        You need at least one backend. Two makes load balancing and failover visible. Two local
        Ollama instances serving the same model:
      </P>
      <Pre>{`OLLAMA_HOST=127.0.0.1:11434 ollama serve &
OLLAMA_HOST=127.0.0.1:11435 ollama serve &
ollama pull llama3`}</Pre>

      <H3>2. Install and run inferoute</H3>
      <P>One command if you have Go, plus the sample config, which already points at those two ports:</P>
      <Pre>{`curl -fsSL https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/install.sh | sh
curl -O https://raw.githubusercontent.com/Rakshit-gen/inferoute/main/config.example.json
inferouted -config config.example.json`}</Pre>
      <P>
        Or build from source with <Code>go build -o bin/inferouted ./cmd/inferouted</Code>. The
        binary takes exactly one flag, <Code>-config</Code>, and defaults to <Code>./config.json</Code>.
      </P>
      <P>
        No GPUs to hand? <Code>./scripts/local-stack.sh</Code> in the repo builds and runs the
        gateway in front of two mock backends that echo prompts back (and can stream), so you can
        try routing, failover, and this dashboard with only Go installed.
      </P>

      <H3>3. Send a request</H3>
      <P>
        Same shape as a call to OpenAI or Ollama. The only change your application ever makes is the
        base URL.
      </P>
      <Pre>{`curl localhost:8081/v1/chat/completions \\
  -d '{"model":"llama3","messages":[{"role":"user","content":"hi"}]}'`}</Pre>
      <P>
        You get back exactly what the backend returned. inferoute only chose which of the two servers
        handled it, which you can confirm from the <Code>X-Inferoute-Backend</Code> response header.
        Kill one <Code>ollama serve</Code> and send the request again: it fails over to the survivor
        instead of erroring.
      </P>

      <H3>4. Watch it here</H3>
      <P>
        Sign in to this dashboard, open <Link href="/connections" className="text-route hover:underline">Connections</Link>,
        and add your gateway&apos;s URL. The dashboard and the{' '}
        <Link href="/playground" className="text-route hover:underline">playground</Link> then read
        from it live. See <a href="#dashboard" className="text-route hover:underline">Using this dashboard</a>.
      </P>

      <H2 id="config">Configuration reference</H2>
      <P>
        Everything is one JSON file passed with <Code>-config</Code>. Every field has a working
        default except <Code>backends</Code>, which is required. A complete example:
      </P>
      <Pre>{`{
  "listen_addr": ":8081",
  "health_check_path": "/",
  "health_check_interval": "10s",
  "backends": [
    { "name": "ollama-1", "url": "http://localhost:11434", "models": ["llama3"] },
    { "name": "ollama-2", "url": "http://localhost:11435", "models": ["llama3"] }
  ],
  "rate_limit": { "enabled": false, "requests_per_second": 5, "burst": 10, "redis_addr": "" },
  "cache": {
    "enabled": false,
    "nucladb_addr": "http://localhost:8080",
    "embedding_backend_addr": "http://localhost:11434",
    "embedding_model": "nomic-embed-text",
    "max_distance": 0.05,
    "tenant_id": "inferoute-cache"
  },
  "model_aliases": {},
  "cors_origins": ["*"],
  "api_keys": []
}`}</Pre>
      <DefList items={configFields} />

      <H2 id="api">HTTP API reference</H2>
      <P>
        Six endpoints. One does the proxying, the rest are read-only introspection used by tools and
        by this dashboard.
      </P>
      <DefList items={endpoints} />

      <H2 id="headers">Response headers</H2>
      <P>
        Every proxied response tells you how it was handled. The{' '}
        <Link href="/playground" className="text-route hover:underline">playground</Link> sends a real
        request and shows both headers along with the path it took.
      </P>
      <DefList items={headers} />

      <H2 id="reload">Changing config without downtime</H2>
      <P>
        Sending the process a <Code>SIGHUP</Code> reloads the <Code>backends</Code>,{' '}
        <Code>model_aliases</Code>, and <Code>api_keys</Code> sections from the same file, with no
        restart and no dropped in-flight requests. This is how you add or drain a backend in
        production.
      </P>
      <Pre>{`kill -HUP $(pgrep inferouted)`}</Pre>
      <P>
        Changes to <Code>rate_limit</Code> and <Code>cache</Code> are not hot-reloaded. Those take a
        full restart.
      </P>

      <H2 id="rate-limiting">Rate limiting in depth</H2>
      <P>
        Rate limiting protects your capacity from a single runaway caller. It is off until you set{' '}
        <Code>rate_limit.enabled</Code> to true.
      </P>
      <P>
        Each caller gets a token bucket. The bucket refills at{' '}
        <Code>requests_per_second</Code> and can hold up to <Code>burst</Code> tokens in reserve.
        Every request spends one token. A caller who has been quiet can spend their whole reserve in a
        short spike, then is held to the steady rate. A caller with an empty bucket gets HTTP 429 and
        should retry after a moment.
      </P>
      <P>
        The bucket is keyed on the caller&apos;s API key, taken from the{' '}
        <Code>Authorization: Bearer</Code> header. Requests with no key share a bucket keyed on the
        client IP address instead.
      </P>
      <P>
        By default the counting happens in memory, inside the one inferoute process. If you run
        several inferoute instances behind a load balancer and want them to enforce one shared limit,
        set <Code>rate_limit.redis_addr</Code> to a Redis address and they will all count against the
        same buckets.
      </P>

      <H2 id="caching">Semantic caching in depth</H2>
      <P>
        The cache lets inferoute answer a question it has effectively seen before without running
        inference again. It is off until you set <Code>cache.enabled</Code> to true, and it needs a
        running <a href="https://github.com/Rakshit-gen/NuclaDB" className="text-route hover:underline">NuclaDB</a>{' '}
        instance to store vectors in.
      </P>
      <P>How a lookup works:</P>
      <ol className="mt-3 space-y-2 text-sm text-ink-dim">
        <li>
          <span className="text-ink">Embed.</span> The prompt text is sent to the embedding model at{' '}
          <Code>cache.embedding_backend_addr</Code> and comes back as a vector.
        </li>
        <li>
          <span className="text-ink">Search.</span> NuclaDB returns the closest stored prompt and its
          distance from this one.
        </li>
        <li>
          <span className="text-ink">Decide.</span> If that distance is below{' '}
          <Code>cache.max_distance</Code>, it is a hit: the stored response is returned as-is, and a
          streamed response is replayed as the exact bytes first captured.
        </li>
        <li>
          <span className="text-ink">Store.</span> On a miss, once the backend has answered, the new
          prompt and response are written to the cache in the background.
        </li>
      </ol>
      <div className="panel mt-4 border-warn/40 p-4">
        <div className="eyebrow mb-2 text-warn">two things that fail silently if set wrong</div>
        <p className="text-sm leading-relaxed text-ink-dim">
          <span className="text-ink">max_distance is a distance, not a similarity.</span> NuclaDB&apos;s
          score is <Code>1 - cosine_similarity</Code>, so <Code>0</Code> means identical and a bigger
          number means further apart. A <span className="text-ink">smaller</span> max_distance is a{' '}
          <span className="text-ink">stricter</span> cache. This is the opposite of what a
          &quot;similarity threshold&quot; would do, and it was the first version&apos;s bug.
        </p>
        <p className="mt-2 text-sm leading-relaxed text-ink-dim">
          <span className="text-ink">NuclaDB&apos;s vector dimension must match your embedding
          model.</span> NuclaDB is started with a fixed vector size for the whole database. Point{' '}
          <Code>cache.embedding_model</Code> at a model whose output is exactly that length, or every
          insert is rejected.
        </p>
      </div>
      <P>
        Measured effect, against the real binary with a backend held at 700ms to stand in for
        inference time: a miss took about 705ms, a hit took about 0.8ms. That is roughly 880 times
        faster on a hit, confirmed from the <Code>X-Inferoute-Cache: hit</Code> header, not from
        timing alone.
      </P>

      <H2 id="metrics">Metrics and observability</H2>
      <P>
        <Code>GET /metrics</Code> serves Prometheus text. Point Prometheus or Grafana at it. The
        collectors:
      </P>
      <DefList
        items={[
          [
            'inferoute_requests_total{model, backend, status}',
            'A counter of every proxied request. "backend" is the server name, or "cache" on a cache hit, or "none" when no backend could be reached. "status" is the HTTP status returned to the caller.',
          ],
          [
            'inferoute_request_duration_seconds{model}',
            'A latency histogram per model, measured from receiving the request to finishing the response. This dashboard reads percentiles off it.',
          ],
          [
            'inferoute_cache_lookups_total{outcome}',
            'A counter of cache lookups by outcome: "hit", "miss", or "error". Hit rate is hits over hits plus misses.',
          ],
        ]}
      />
      <P>
        The <Link href="/dashboard" className="text-route hover:underline">dashboard</Link> renders
        all of this live: throughput, latency, per-backend share, and cache effectiveness.
      </P>

      <H2 id="deploying">Deploying to production</H2>
      <P>
        inferoute is a stateless HTTP proxy, so it deploys like any other small web service, with no
        database or persistent disk of its own. A container image is provided (<Code>Dockerfile</Code>{' '}
        at the repo root).
      </P>
      <ul className="mt-3 space-y-2 text-sm text-ink-dim">
        <li>
          Put your real <Code>config.json</Code> on the host as a secret file and point{' '}
          <Code>-config</Code> at it. The image&apos;s default command already expects it at{' '}
          <Code>/etc/secrets/config.json</Code>.
        </li>
        <li>Use <Code>/healthz</Code> as the health check path for your load balancer or platform.</li>
        <li>
          Your backends must be reachable from wherever inferoute runs. A <Code>localhost</Code> URL
          only works if the backend is on the same host. Use private networking or public addresses.
        </li>
        <li>
          inferoute does not terminate TLS. Put it behind a reverse proxy or your platform&apos;s
          load balancer for HTTPS.
        </li>
        <li>
          The read-only endpoints (<Code>/v1/backends</Code>, <Code>/v1/config</Code>,{' '}
          <Code>/metrics</Code>) are not behind <Code>api_keys</Code>. If they should not be public,
          firewall them or keep them on an internal network.
        </li>
      </ul>

      <H2 id="dashboard">Using this dashboard</H2>
      <P>
        This dashboard is a separate application from the gateway. The gateway serves one tenant; the
        dashboard lets several people each watch their own gateways without seeing each other&apos;s.
      </P>
      <ol className="mt-3 space-y-2 text-sm text-ink-dim">
        <li>
          <span className="text-ink">Sign in.</span> Use the Sign up button in the nav. Authentication
          is handled by Clerk.
        </li>
        <li>
          <span className="text-ink">Add a connection.</span> On the{' '}
          <Link href="/connections" className="text-route hover:underline">Connections</Link> page,
          enter your gateway&apos;s base URL and, if it has an <Code>api_keys</Code> allowlist, a key.
          The key is stored server-side and never sent to the browser.
        </li>
        <li>
          <span className="text-ink">Watch it.</span> The Dashboard and Playground read from your
          active connection. You can register several gateways and switch between them.
        </li>
      </ol>
      <P>
        Every request the dashboard makes to a gateway goes through this app&apos;s own server, which
        attaches your connection and its key. One account&apos;s requests can never be routed to
        another account&apos;s gateway. Because the browser talks only to this app and never to a
        gateway directly, the gateway&apos;s <Code>cors_origins</Code> does not need to include the
        dashboard.
      </P>

      <H2 id="limits">Current limitations</H2>
      <P>Things inferoute does not do yet, so you can plan around them:</P>
      <ul className="mt-3 space-y-2 text-sm text-ink-dim">
        <li>Routing is round-robin only. No weighting by capacity, and no least-latency choice.</li>
        <li>
          A backend is either healthy or not. There is no circuit breaker for a server that is up but
          flaky.
        </li>
        <li>
          The cache has no eviction or TTL. A long-running gateway&apos;s cache tenant grows forever,
          so prune it out of band for now.
        </li>
        <li>Cache writes are one per miss, done in the background. No batching for very high miss rates.</li>
        <li>No built-in TLS. Terminate it in front.</li>
      </ul>

      <footer className="mt-16 border-t border-line pt-6 text-xs text-ink-dim">
        inferoute ·{' '}
        <a href={GITHUB} className="hover:text-ink">
          github.com/Rakshit-gen/inferoute
        </a>
      </footer>
    </article>
  )
}
