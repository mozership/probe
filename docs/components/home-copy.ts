export type Locale = 'en' | 'ja'

type Part = { name: string; description: string; code: string }

type Feature = Part & { label: string }

export type HomeCopy = {
  headline: string[]
  lede: string
  primaryCta: { label: string; href: string }
  secondaryCta: { label: string; href: string }
  heroWorkflowLabel: string
  exampleHeading: string
  heroDagAlt: string
  flowNote: string
  heroReportLabel: string
  channels: {
    heading: string
    body: string
    chart: {
      title: string
      runner: string
      runnerNote: string
      builtInLabel: string
      externalLabel: string
      externalNames: string[]
      transport: string
    }
    link: { label: string; href: string }
  }
  graph: {
    heading: string
    body: string
    chart: {
      title: string
      workflowLabel: string
      jobLabel: string
      stepLabel: string
      needsLabel: string
    }
    link: { label: string; href: string }
  }
  shared: {
    heading: string
    body: string
    workflowLabel: string
    jobLabel: string
    link: { label: string; href: string }
  }
  parts: { heading: string; body: string; items: Part[] }
  browser: {
    heading: string
    body: string
    workflowLabel: string
    reportLabel: string
    note: string
    link: { label: string; href: string }
  }
  agents: {
    heading: string
    body: string
    workflowLabel: string
    reportLabel: string
    checkNote: string
    items: Feature[]
    link: { label: string; href: string }
  }
  install: { heading: string; body: string; sourceLabel: string; docsLink: string; docsHref: string }
  copy: { idle: string; done: string }
}

export const homeCopy: Record<Locale, HomeCopy> = {
  en: {
    headline: ['Proof that AI-written code works.'],
    lede: 'When an AI coding agent changes a service, something other than the code and tests it wrote has to show that the change works. Probe runs a YAML workflow, written like GitHub Actions, that drives the running service over HTTP, gRPC, databases, mail, SSH and shell, and checks each result. The agent writes the workflow, Probe checks it before it runs, and runs it without writes or connections you did not allow.',
    primaryCta: { label: 'Install Probe', href: '/guide/introduction/installation' },
    secondaryCta: { label: 'Read the quickstart', href: '/guide/introduction/quickstart' },
    heroWorkflowLabel: 'signup-flow.yml',
    exampleHeading: 'An example sign-up scenario',
    heroDagAlt: 'Create account runs first; Database and Mailbox both follow it and run in parallel; Activate waits for both.',
    flowNote: 'Create account posts the form and publishes the id that comes back. Database and Mailbox both depend on it, so Probe runs them at the same time — one asks SQLite for the row, the other opens the mailbox over IMAP. Activate waits for both before following the confirmation link.',
    heroReportLabel: '$ probe signup-flow.yml',
    channels: {
      heading: 'Supported actions',
      body: 'A step names an action in uses, and Probe starts that action as its own process and talks to it over gRPC. The built-in actions go through exactly the same handshake an action you write does, so none of them is a special case. What Probe cannot reach yet, you can add.',
      chart: {
        title: 'Probe talks to every action over gRPC, whether it is built in or your own.',
        runner: 'probe',
        runnerNote: 'workflow runner',
        builtInLabel: 'Built in',
        externalLabel: 'Yours',
        externalNames: ['agent', 'graphql', 'jmap'],
        transport: 'gRPC',
      },
      link: { label: 'Read the actions reference', href: '/reference/actions-reference' },
    },
    graph: {
      heading: 'What a workflow is made of',
      body: 'A workflow is a set of jobs, and a job is a sequence of steps. Steps run one after another, in the order you wrote them. Jobs run in parallel by default; only the ones with needs wait for what they depend on. Add repeat to a job and the same file runs on an interval and reports a success rate.',
      chart: {
        title: 'A workflow holds jobs; a job holds steps that run in order. Two jobs run side by side, and a third waits for both through needs.',
        workflowLabel: 'Workflow',
        jobLabel: 'Job',
        stepLabel: 'Step',
        needsLabel: 'needs',
      },
      link: { label: 'Read the jobs and steps guide', href: '/guide/concepts/jobs-and-steps' },
    },
    shared: {
      heading: 'Shared steps, in a file of their own',
      body: 'A scenario usually starts by logging in, and every scenario after the first repeats it. Put those steps in a job file and any workflow runs it with uses: embedded, handing it vars and reading its outputs back. The job\'s steps appear nested under the step that called it, so a failure still says which one broke.',
      workflowLabel: 'orders.yml',
      jobLabel: 'login-job.yml',
      link: { label: 'Read the embedded action reference', href: '/reference/actions/embedded' },
    },
    parts: {
      heading: 'What a step can say',
      body: 'Whatever action a step names, these fields are available to it.',
      items: [
        {
          name: 'test',
          description: 'A step passes when the expression is true. Everything about the response is in scope.',
          code: 'test: res.code == 200 && res.body.status == "ok"',
        },
        {
          name: 'outputs',
          description: 'Name a value once, then read it from any later step or job.',
          code: 'outputs:\n  token: res.body.access_token',
        },
        {
          name: 'iteration',
          description: 'Run one step once per set of variables, without copying it.',
          code: 'iteration:\n- {name: Alice, role: admin}\n- {name: Bob, role: user}',
        },
        {
          name: 'retry',
          description: 'Give a flaky step another go, after a pause.',
          code: 'retry:\n  max_attempts: 3\n  interval: 5s',
        },
        {
          name: 'skipif',
          description: 'Leave the step out when the condition holds.',
          code: 'skipif: vars.env == "local"',
        },
        {
          name: 'wait',
          description: 'Pause before the step runs, for whatever has to settle first.',
          code: 'wait: 5s',
        },
      ],
    },
    browser: {
      heading: 'Test the UI in natural language',
      body: 'Write what a user does in task, and what has to hold afterwards in expect. The agent action has a model carry out the task in a real Chrome, and another call of the model judge it from what the pages showed, quoting them. The steps of a run that passed are recorded, and the next run replays them without the model. It runs with models of Anthropic, OpenAI, xAI, Gemini or the Sakura AI Engine.',
      workflowLabel: 'signup.yml',
      reportLabel: '$ probe signup.yml',
      note: 'This is the second run. In the first, the model signed up in 10 steps, which were recorded in signup.json; this one replayed them without the model, and only the judge was asked, so each quote was still checked against what the pages showed.',
      link: { label: 'Read the agent action reference', href: '/reference/actions/agent' },
    },
    agents: {
      heading: 'Built for coding agents',
      body: 'When an agent writes the code and the tests, something has to check both. Probe teaches the agent how to use the version it runs, finds mistakes in a workflow before it runs, judges responses by your spec rather than by the agent\'s own test, and refuses writes, and connections to hosts you did not allow, before they are sent.',
      workflowLabel: 'agent-workflow.yml',
      reportLabel: '$ probe check agent-workflow.yml',
      checkNote: 'probe check reads a workflow without running it. A run ignores a key it does not know, in a step or in with, so the step can pass for the wrong reason, and finds a wrong output name only when the step reaches it. Here each one is an error with its line before anything is sent, and a step that nothing checks is a warning.',
      items: [
        {
          name: 'skill',
          label: 'shell',
          description: 'Install a skill that teaches the agent to write, run and debug workflows. It reads probe guide, the reference built into the binary, so what it writes fits the version it runs.',
          code: 'probe skill install\nprobe guide http',
        },
        {
          name: 'openapi',
          label: 'yaml',
          description: 'Check each request and response against your OpenAPI document. The spec decides what is right, not the test the agent wrote, so a wrong test cannot agree with a wrong response.',
          code: 'uses: http\nwith:\n  get: /users/1\n  openapi:\n    spec: ./openapi.yml',
        },
        {
          name: 'proto',
          label: 'yaml',
          description: 'The same for gRPC, from your .proto files, with the rules of buf.validate and google.api.field_behavior.',
          code: 'uses: grpc\nwith:\n  method: GetUser\n  proto:\n    files: [./proto/user/v1/user.proto]',
        },
        {
          name: 'coverage',
          label: 'shell',
          description: 'Tell which operations, responses and methods the run checked, and which ones no step reached yet.',
          code: 'probe --report json workflow.yml\nprobe coverage openapi.yml \\\n  probe-report.json',
        },
        {
          name: '--read-only',
          label: 'shell',
          description: 'Run what the agent wrote without letting it write: anything but reads, and any host not listed, is refused before it is sent.',
          code: 'probe --read-only \\\n  --allow-host api.staging.example.com \\\n  workflow.yml',
        },
        {
          name: 'exit code',
          label: 'shell',
          description: 'The exit code says what to look at: a failed test or broken contract, the workflow itself, or a target that did not answer.',
          code: 'probe workflow.yml\necho $?  # 1 test, 2 workflow, 3 action',
        },
      ],
      link: { label: 'Set up a project for an agent', href: '/guide/how-tos/coding-agents' },
    },
    install: {
      heading: 'Install',
      body: 'Probe is a single binary with no runtime dependencies. Homebrew installs it with one command.',
      sourceLabel: 'Or install with Go',
      docsLink: 'Read the installation guide',
      docsHref: '/guide/introduction/installation',
    },
    copy: { idle: 'Copy', done: 'Copied' },
  },
  ja: {
    headline: ['AIが書いたコードに', '動く証拠を'],
    lede: 'AIエージェントがサービスを変更したとき、その変更が動くことは、AIエージェント自身が書いたコードやテストとは別のもので示す必要があります。ProbeはGitHub ActionsのようなYAMLで定義したWorkflowを実行し、動いているサービスをHTTP、gRPC、データベース、メール、SSH、シェル越しに操作して、結果を検証します。WorkflowはAIエージェントが書き、Probeが実行前に検査し、許可していない書き込みや接続を防いで実行します。',
    primaryCta: { label: 'Probeをインストール', href: '/ja/guide/introduction/installation' },
    secondaryCta: { label: 'クイックスタートを読む', href: '/ja/guide/introduction/quickstart' },
    heroWorkflowLabel: 'signup-flow.yml',
    exampleHeading: 'サインアップのシナリオの例',
    heroDagAlt: 'Create accountが先に走り、DatabaseとMailboxがそれに続いて並列に走り、Activateが両方の完了を待つ。',
    flowNote: '最初にCreate accountがフォームを送信し、返ってきたidを後続へ渡します。これを待つのがDatabaseとMailboxで、この2つは互いに依存しないため同時に走ります。DatabaseはSQLiteにレコードの状態を問い合わせ、MailboxはIMAPでメールボックスを開いて確認メールを探します。Activateはその両方が終わるのを待ってから、確認リンクをたどります。',
    heroReportLabel: '$ probe signup-flow.yml',
    channels: {
      heading: 'サポートしているアクション',
      body: 'Stepのusesにアクション名を書くと、Probeはそのアクションを別プロセスとして起動し、gRPC越しにやり取りします。ビルトインのアクションも、自作のアクションとまったく同じ手順で接続されます。特別扱いされているものは1つもないので、必要なアクションが無ければ自分で追加できます。',
      chart: {
        title: 'Probeはビルトインのアクションも自作のアクションも、同じgRPCでつないでいる。',
        runner: 'probe',
        runnerNote: 'ワークフロー実行',
        builtInLabel: 'ビルトイン',
        externalLabel: '自作のアクション',
        externalNames: ['agent', 'graphql', 'jmap'],
        transport: 'gRPC',
      },
      link: { label: 'アクションリファレンスを読む', href: '/ja/reference/actions/variables' },
    },
    graph: {
      heading: 'Workflowの構成要素',
      body: 'WorkflowはJobの集まりで、JobはStepの並びです。Stepは書いた順に1つずつ走ります。Jobは既定で並列に走り、needsを書いたものだけが依存先の完了を待ちます。Jobにrepeatを足せば、同じファイルが一定間隔で走り、成功率を返します。',
      chart: {
        title: 'WorkflowがJobを持ち、JobがStepを順に持つ。2つのJobは並んで走り、3つ目がneedsで両方の完了を待つ。',
        workflowLabel: 'Workflow',
        jobLabel: 'Job',
        stepLabel: 'Step',
        needsLabel: 'needs',
      },
      link: { label: 'ジョブとステップを読む', href: '/ja/guide/concepts/jobs-and-steps' },
    },
    shared: {
      heading: '共通処理は別のファイルに',
      body: 'シナリオの多くはログインから始まり、2つ目以降のシナリオでも同じ手順を書くことになります。その手順をジョブファイルに置けば、どのWorkflowからもuses: embeddedで実行でき、varsを渡してoutputsを受け取れます。呼び出したジョブのStepはレポートでも入れ子で表示されるため、失敗したときにどのStepかがわかります。',
      workflowLabel: 'orders.yml',
      jobLabel: 'login-job.yml',
      link: { label: 'embeddedアクションのリファレンスを読む', href: '/ja/reference/actions/embedded' },
    },
    parts: {
      heading: 'Stepで使用できる便利な機能',
      body: 'Stepにはアクションの設定以外に、以下の共通機能があります。',
      items: [
        {
          name: 'test',
          description: '式が真ならそのStepは成功です。レスポンスの中身はすべて参照できます。',
          code: 'test: res.code == 200 && res.body.status == "ok"',
        },
        {
          name: 'outputs',
          description: '値に名前をつけておくと、あとのStepやJobから読めます。',
          code: 'outputs:\n  token: res.body.access_token',
        },
        {
          name: 'iteration',
          description: '変数の組ごとに、同じStepをコピーせず繰り返します。',
          code: 'iteration:\n- {name: Alice, role: admin}\n- {name: Bob, role: user}',
        },
        {
          name: 'retry',
          description: '失敗したときに、間隔をあけてやり直します。',
          code: 'retry:\n  max_attempts: 3\n  interval: 5s',
        },
        {
          name: 'skipif',
          description: '条件に当てはまるときは、そのStepを飛ばします。',
          code: 'skipif: vars.env == "local"',
        },
        {
          name: 'wait',
          description: '先に落ち着くのを待ちたいとき、実行前に待機します。',
          code: 'wait: 5s',
        },
      ],
    },
    browser: {
      heading: '画面のテストは自然言語で',
      body: 'ユーザーが行うことをtaskに、終わったあとに成り立つべきことをexpectに書きます。agentアクションでは、モデルが実際のChromeでタスクを行い、別に呼び出したモデルがページに表示された内容を引用して判定します。通った実行の手順は記録し、次の実行ではモデルを使わずに再生します。Anthropic、OpenAI、xAI、Gemini、さくらのAI Engineのモデルで動きます。',
      workflowLabel: 'signup.yml',
      reportLabel: '$ probe signup.yml',
      note: 'これは2回目の実行です。1回目はモデルが10手で登録し、その手順をsignup.jsonに記録しました。2回目はモデルを使わずにそれを再生し、モデルには判定だけを頼んだので、引用は今回もページに表示された内容と照合しています。',
      link: { label: 'agentアクションのリファレンスを読む', href: '/ja/reference/actions/agent' },
    },
    agents: {
      heading: 'AIエージェントのために',
      body: 'AIエージェントがコードとテストを書くなら、その両方を確かめる仕組みが要ります。Probeは動いているバージョンの使い方をAIエージェントに教え、Workflowの誤りを実行前に見つけ、レスポンスの正しさをAIエージェント自身のtestではなく仕様で判定し、書き込みや許可していないホストへの接続を送信前に拒否します。',
      workflowLabel: 'agent-workflow.yml',
      reportLabel: '$ probe check agent-workflow.yml',
      checkNote: 'probe checkはWorkflowを実行せずに読みます。Stepやwithのキーの綴りを間違えると、実行時には無視されて別の理由で通ってしまうことがあります。outputsの名前の誤りは、実行してそのStepに来るまでわかりません。ここではどれも何かを送る前に行番号つきのエラーになり、何も確かめていないStepは警告になります。',
      items: [
        {
          name: 'skill',
          label: 'shell',
          description: 'Workflowの書き方、実行、失敗の読み方を教えるスキルを入れます。AIエージェントはバイナリに組み込まれたリファレンスをprobe guideで読むので、動いているバージョンに合ったYAMLを書きます。',
          code: 'probe skill install\nprobe guide http',
        },
        {
          name: 'openapi',
          label: 'yaml',
          description: 'リクエストとレスポンスをOpenAPIのドキュメントと照合します。正しさを決めるのはAIエージェントが書いたtestではなく仕様なので、間違ったtestが間違ったレスポンスを通すことはありません。',
          code: 'uses: http\nwith:\n  get: /users/1\n  openapi:\n    spec: ./openapi.yml',
        },
        {
          name: 'proto',
          label: 'yaml',
          description: 'gRPCでも同じく.protoファイルと照合します。buf.validateのルールとgoogle.api.field_behaviorも確かめます。',
          code: 'uses: grpc\nwith:\n  method: GetUser\n  proto:\n    files: [./proto/user/v1/user.proto]',
        },
        {
          name: 'coverage',
          label: 'shell',
          description: '実行したStepが仕様のどのオペレーション、レスポンス、メソッドを確かめたか、まだどれが残っているかを示します。',
          code: 'probe --report json workflow.yml\nprobe coverage openapi.yml \\\n  probe-report.json',
        },
        {
          name: '--read-only',
          label: 'shell',
          description: 'AIエージェントが書いたWorkflowを、書き込ませずに走らせます。読み取り以外の操作と、許可していないホストへの接続は、送る前に拒否します。',
          code: 'probe --read-only \\\n  --allow-host api.staging.example.com \\\n  workflow.yml',
        },
        {
          name: 'exit code',
          label: 'shell',
          description: '終了コードで何を調べればよいかがわかります。testや仕様の違反か、Workflow自体の誤りか、相手が応答しなかったかを区別します。',
          code: 'probe workflow.yml\necho $?  # 1 test, 2 workflow, 3 action',
        },
      ],
      link: { label: 'AIエージェント向けにプロジェクトを設定する', href: '/ja/guide/how-tos/coding-agents' },
    },
    install: {
      heading: 'インストール',
      body: 'Probeは実行時の依存がない、単体のバイナリです。Homebrewなら1つのコマンドで入ります。',
      sourceLabel: 'Goでインストールする場合',
      docsLink: 'インストールガイドを読む',
      docsHref: '/ja/guide/introduction/installation',
    },
    copy: { idle: 'コピー', done: 'コピーしました' },
  },
}
