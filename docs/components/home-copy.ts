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
    kicker: string
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
    kicker: string
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
    kicker: string
    heading: string
    body: string
    workflowLabel: string
    jobLabel: string
    link: { label: string; href: string }
  }
  parts: { kicker: string; heading: string; body: string; items: Part[] }
  browser: {
    kicker: string
    heading: string
    body: string
    workflowLabel: string
    reportLabel: string
    note: string
    providers: { label: string; items: { id: string; name: string }[] }
    link: { label: string; href: string }
  }
  agents: {
    kicker: string
    heading: string
    body: string
    workflowLabel: string
    reportLabel: string
    checkNote: string
    items: Feature[]
    link: { label: string; href: string }
  }
  install: { kicker: string; heading: string; body: string; sourceLabel: string; docsLink: string; docsHref: string }
  faq: {
    kicker: string
    heading: string
    items: { question: string; answer: string; link: { label: string; href: string } }[]
  }
  copy: { idle: string; done: string }
}

export const homeCopy: Record<Locale, HomeCopy> = {
  en: {
    headline: ['Continuous product verification', 'for AI development'],
    lede: 'With AI agents writing code, changes come faster and in greater numbers. Code and tests that pass do not show that a user can do what they came to do. Probe takes end-to-end testing from a single page or API to the whole customer journey. A YAML workflow, written like GitHub Actions, follows a sign-up in the browser to the mail it sends, the row it writes and a sign-in on the iOS app, and an AI agent playing a first-time user judges whether they reach their goal. The same workflow runs in CI on every change and on an interval after release, so you keep knowing that the experience still works.',
    primaryCta: { label: 'Install Probe', href: '/guide/introduction/installation' },
    secondaryCta: { label: 'Read the quickstart', href: '/guide/introduction/quickstart' },
    heroWorkflowLabel: 'signup-flow.yml',
    exampleHeading: 'An example sign-up scenario',
    heroDagAlt: 'Sign up runs first in a browser; API, Database and Mailbox follow it and run in parallel; Activate waits for all three; iOS app and First visit follow it and run in parallel.',
    flowNote: 'Sign up fills in the form in Chrome. API, Database and Mailbox each check what it left behind, at the same time: the API refuses to sign in an address not yet confirmed, SQLite holds the row as pending, and the confirmation mail is in the mailbox over IMAP. Activate waits for all three and opens the link from the mail in the browser. Then two jobs use the account at once: iOS app signs in on an iPhone simulator, and First visit hands the web app to an AI agent playing Ada, who has never seen it, and judges whether she reaches her goal — what a test written step by step cannot tell you.',
    heroReportLabel: '$ probe signup-flow.yml',
    channels: {
      kicker: 'Architecture',
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
      kicker: 'Structure',
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
      kicker: 'Reuse',
      heading: 'Shared steps, in a file of their own',
      body: 'A scenario usually starts by logging in, and every scenario after the first repeats it. Put those steps in a job file and any workflow runs it with uses: embedded, handing it vars and reading its outputs back. The job\'s steps appear nested under the step that called it, so a failure still says which one broke.',
      workflowLabel: 'orders.yml',
      jobLabel: 'login-job.yml',
      link: { label: 'Read the embedded action reference', href: '/reference/actions/embedded' },
    },
    parts: {
      kicker: 'Syntax',
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
      kicker: 'Quality',
      heading: 'What deterministic tests leave out',
      body: 'The steps above check what can be stated exactly, and they are what should gate a merge. Whether a first-time user gets through, or whether the application still holds up once data and settings pile up on staging, cannot be written as an expression. The agent action takes it in natural language, as a persona and a goal: a model plays the persona in a real Chrome, and another call of the model reviews the run, keeping only what the pages and the requests bear out. Run it with repeat against staging, and it keeps using the application as people do.',
      workflowLabel: 'first-note.yml',
      reportLabel: '$ probe first-note.yml',
      note: 'The first finding is a server error, which an http step would have caught had someone written one. The second is in no test: after signing up, nothing tells Carol she is signed in, nor how to get to her notes. The test of the step asks only that the review ran; the verdict and the findings are the result to read.',
      providers: {
        label: 'Works with',
        items: [{ id: 'claude', name: 'Claude' }, { id: 'openai', name: 'OpenAI' }, { id: 'gemini', name: 'Gemini' }, { id: 'xai', name: 'xAI' }, { id: 'sakura', name: 'Sakura AI Engine' }],
      },
      link: { label: 'Read the agent action reference', href: '/reference/actions/agent' },
    },
    agents: {
      kicker: 'Coding agents',
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
      kicker: 'Get started',
      heading: 'Install',
      body: 'Probe is a single binary with no runtime dependencies. Homebrew installs it with one command.',
      sourceLabel: 'Or install with Go',
      docsLink: 'Read the installation guide',
      docsHref: '/guide/introduction/installation',
    },
    faq: {
      kicker: 'FAQ',
      heading: 'Questions you may have',
      items: [
        {
          question: 'Do I need an AI coding agent to use Probe?',
          answer: 'No. A workflow is plain YAML, and Probe runs it the same whoever wrote it. What Probe does for an agent, such as finding the mistakes in a workflow before it runs, it does for a person writing one by hand too.',
          link: { label: 'Read the quickstart', href: '/guide/introduction/quickstart' },
        },
        {
          question: 'How is it different from runn, Hurl or k6?',
          answer: 'Probe drives a browser, an iOS app, HTTP, databases and mail from one YAML file, runs its jobs in parallel where needs allows, and is a single binary. It does not put load on a system, which is what k6 is for, and for HTTP alone Hurl is the lighter tool.',
          link: { label: 'Compare it with similar tools', href: '/guide/introduction/comparison' },
        },
        {
          question: 'Can it run in CI?',
          answer: 'Yes. On GitHub Actions, mozership/probe-action downloads Probe, verifies its checksum, caches it and runs your workflows. The exit code says what to look at: 1 for a failed test, 2 for the workflow itself, 3 for a target that did not answer. The iOS action needs a macOS runner, where Probe is installed with go install.',
          link: { label: 'Set it up in GitHub Actions', href: '/guide/introduction/installation' },
        },
        {
          question: 'Can a workflow an agent wrote change a system it should not?',
          answer: 'Not when you run it under the guard. --read-only refuses what writes, and --allow-host refuses connecting to a host you did not name, before anything is sent; nothing in a workflow can loosen either. An action that cannot tell what it will do, such as the browser or the agent action, is refused under the guard unless --allow-action names it.',
          link: { label: 'Read about the guard', href: '/guide/concepts/guard' },
        },
        {
          question: 'What is the agent action for, when its result is not deterministic?',
          answer: 'For what a deterministic step cannot state. A status, a row or a mail can be checked exactly, and those steps should gate a merge. Whether a first-time user gets through a page, or whether the application still holds up on staging once data and settings pile up, cannot be written as an expected value. The agent action leaves that to a model using the application through a real Chrome, and adds to the deterministic steps rather than replacing them.',
          link: { label: 'See what deterministic tests leave out', href: '/reference/actions/agent' },
        },
        {
          question: 'The agent action asks a model. Does its result change from run to run?',
          answer: 'It can. The judge quotes what the pages showed and marks whether each quote was found there, and blocked, a test that could not be judged, is kept apart from fail. A scenario test can keep the steps of a run that passed and replay them without the model; only the judge is asked again.',
          link: { label: 'Read the agent action', href: '/reference/actions/agent' },
        },
        {
          question: 'What does testing an iOS or Android app need?',
          answer: 'The iOS action runs on macOS with Xcode, against a booted simulator with the app installed. The Android action runs on Linux and macOS through adb, with an emulator or a device, and nothing but the app is installed on it. Neither boots the device or installs the app: do that in a shell step before.',
          link: { label: 'Read the iOS action', href: '/reference/actions/ios' },
        },
        {
          question: 'What if the protocol I need is not supported?',
          answer: 'Write an action. Probe talks to an action over gRPC, so it can be written in any language, and the built-in actions go through the same handshake. A workflow pins an external action to a full commit SHA, and Probe runs only the executable whose SHA-256 the action.yml at that commit names.',
          link: { label: 'Read about actions', href: '/guide/concepts/actions' },
        },
        {
          question: 'Is it free?',
          answer: 'Yes. Probe is open source under the MIT License. The agent action calls the model provider you choose with your own API key, and the provider bills that use.',
          link: { label: 'See it on GitHub', href: 'https://github.com/mozership/probe' },
        },
      ],
    },
    copy: { idle: 'Copy', done: 'Copied' },
  },
  ja: {
    headline: ['AI開発に継続的な', 'プロダクト品質検証を'],
    lede: 'AIエージェントと開発すると、変更は速く、多くなります。コードとテストが通っても、ユーザーが目的を果たせるとは限りません。ProbeはE2Eテストの範囲を、1つの画面やAPIからカスタマージャーニー全体に広げます。ブラウザでのサインアップから、届いたメール、データベースの行、iOSアプリでのサインインまでをGitHub ActionsのようなYAMLのWorkflowで検証し、初めて使う人をAIエージェントに演じさせて、目的を果たせたかも判定します。同じWorkflowを変更のたびにCIで、リリースのあとも一定間隔で実行し、体験が壊れていないことを確かめ続けます。',
    primaryCta: { label: 'Probeをインストール', href: '/ja/guide/introduction/installation' },
    secondaryCta: { label: 'クイックスタートを読む', href: '/ja/guide/introduction/quickstart' },
    heroWorkflowLabel: 'signup-flow.yml',
    exampleHeading: 'サインアップのシナリオの例',
    heroDagAlt: 'Sign upが先にブラウザで走り、API、Database、Mailboxがそれに続いて並列に走り、Activateが3つすべての完了を待ち、iOS appとFirst visitがそれに続いて並列に走る。',
    flowNote: '最初にSign upがChromeでフォームに入力して送信します。API、Database、Mailboxはその結果を同時に確かめます。APIは確認前のアドレスでのサインインを拒否し、SQLiteのレコードはpendingのままで、IMAPで開いたメールボックスには確認メールが届いています。Activateはこの3つを待ってから、メールのリンクをブラウザで開きます。そのあとの2つは同時に走ります。iOS appはiPhoneのシミュレータからサインインし、First visitは初めてアプリを使うAdaをAIエージェントに演じさせて、目的を果たせたかを判定させます。手順を1つずつ書くテストでは確かめられない、使いやすさの検証です。',
    heroReportLabel: '$ probe signup-flow.yml',
    channels: {
      kicker: '仕組み',
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
      kicker: '構造',
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
      kicker: '再利用',
      heading: '共通処理は別のファイルに',
      body: 'シナリオの多くはログインから始まり、2つ目以降のシナリオでも同じ手順を書くことになります。その手順をジョブファイルに置けば、どのWorkflowからもuses: embeddedで実行でき、varsを渡してoutputsを受け取れます。呼び出したジョブのStepはレポートでも入れ子で表示されるため、失敗したときにどのStepかがわかります。',
      workflowLabel: 'orders.yml',
      jobLabel: 'login-job.yml',
      link: { label: 'embeddedアクションのリファレンスを読む', href: '/ja/reference/actions/embedded' },
    },
    parts: {
      kicker: '記法',
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
      kicker: '品質',
      heading: '決定論的なテストでは書けない品質を',
      body: 'ここまでのStepは厳密に書ける正しさを確かめるもので、マージの関門になるべきものです。一方で、初めての人が最後までたどり着けるか、データや設定が積み重なったステージングでもアプリケーションが持ちこたえるかは、式では書けません。agentアクションでは、それをペルソナと目標として自然言語で書きます。モデルが実際のChromeでペルソナを演じ、別に呼び出したモデルが、ページとリクエストの記録に裏付けられた指摘だけを残します。repeatを付けてステージングで回せば、人と同じようにアプリケーションを使い続けます。',
      workflowLabel: 'first-note.yml',
      reportLabel: '$ probe first-note.yml',
      note: '1つ目の指摘はサーバーのエラーで、誰かがhttpのStepを書いていれば見つけられたものです。2つ目はどのテストにも書かれていません。登録したあと、ログインしていることも、メモへたどり着く道も、Carolには示されていません。このStepのtestはレビューが実行できたことだけを確かめ、判定と指摘は読むための結果として返ります。',
      providers: {
        label: '使えるモデル',
        items: [{ id: 'claude', name: 'Claude' }, { id: 'openai', name: 'OpenAI' }, { id: 'gemini', name: 'Gemini' }, { id: 'xai', name: 'xAI' }, { id: 'sakura', name: 'さくらのAI Engine' }],
      },
      link: { label: 'agentアクションのリファレンスを読む', href: '/ja/reference/actions/agent' },
    },
    agents: {
      kicker: 'AIエージェント',
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
      kicker: 'はじめる',
      heading: 'インストール',
      body: 'Probeは実行時の依存がない、単体のバイナリです。Homebrewなら1つのコマンドで入ります。',
      sourceLabel: 'Goでインストールする場合',
      docsLink: 'インストールガイドを読む',
      docsHref: '/ja/guide/introduction/installation',
    },
    faq: {
      kicker: 'FAQ',
      heading: 'よくある質問',
      items: [
        {
          question: 'AIエージェントを使っていなくても使えますか？',
          answer: '使えます。WorkflowはただのYAMLで、誰が書いてもProbeは同じように実行します。実行前にWorkflowの誤りを見つけるといった、ProbeがAIエージェントのためにしていることは、人が手で書くときにもそのまま役立ちます。',
          link: { label: 'クイックスタートを読む', href: '/ja/guide/introduction/quickstart' },
        },
        {
          question: 'runn、Hurl、k6とはどう違いますか？',
          answer: 'Probeは1つのYAMLから、ブラウザ、iOSアプリ、HTTP、データベース、メールを操作し、needsが許す範囲でJobを並列に走らせます。実体は単一のバイナリです。負荷をかけることはできないので、性能の測定にはk6を使ってください。HTTPだけならHurlのほうが軽く書けます。',
          link: { label: '類似ツールと比べる', href: '/ja/guide/introduction/comparison' },
        },
        {
          question: 'CIで動かせますか？',
          answer: '動かせます。GitHub Actionsでは、mozership/probe-actionがProbeをダウンロードし、チェックサムを検証してキャッシュし、Workflowを実行します。終了コードで何を調べればよいかがわかり、1はtestの失敗、2はWorkflow自体の誤り、3は相手が応答しなかったことを表します。iOSアクションにはmacOSのランナーが必要で、そこではgo installでProbeを入れます。',
          link: { label: 'GitHub Actionsで使う', href: '/ja/guide/introduction/installation' },
        },
        {
          question: 'AIエージェントが書いたWorkflowが、触ってはいけないシステムを変えてしまいませんか？',
          answer: 'ガードを付けて実行すれば防げます。--read-onlyは書き込みを、--allow-hostは指定していないホストへの接続を、送信する前に拒否します。Workflowの中からこれを緩めることはできません。ブラウザやagentのように、何をするかを事前に判断できないアクションは、--allow-actionで名前を挙げない限りガードの下では拒否されます。',
          link: { label: 'ガードについて読む', href: '/ja/guide/concepts/guard' },
        },
        {
          question: '決定論的でないagentアクションは何のためにあるのですか？',
          answer: '決定論的なStepでは書けないことを確かめるためです。ステータスやレコードやメールは正確に検証でき、マージの可否はそうしたStepで決めるべきです。一方で、初めての利用者がページを迷わず進めるか、データや設定が積み重なったステージングでもアプリケーションが持ちこたえるかは、期待値として書けません。agentアクションはそれを、本物のChromeでアプリケーションを使うモデルに任せます。決定論的なStepを置き換えるものではなく、その外側を補うものです。',
          link: { label: '決定論的なテストが取りこぼすもの', href: '/ja/reference/actions/agent' },
        },
        {
          question: 'agentアクションはモデルに判断させます。結果は毎回変わりませんか？',
          answer: '変わることがあります。判定ではページに表示された内容を引用し、その引用が実際にページにあったかを記録します。判定できなかったことを表すblockedは、failと区別されます。シナリオテストなら、通った実行の手順を保存し、モデルを使わずに再生できます。その場合も判定だけはモデルに改めて任せます。',
          link: { label: 'agentアクションを読む', href: '/ja/reference/actions/agent' },
        },
        {
          question: 'iOSやAndroidのアプリをテストするには何が必要ですか？',
          answer: 'iOSアクションはXcodeの入ったmacOSで動き、アプリを入れて起動したシミュレータを操作します。AndroidアクションはLinuxとmacOSでadb越しに動き、エミュレータか実機を操作します。端末に入れるのはテストするアプリだけです。どちらも端末の起動やアプリのインストールはしないので、その前のshellのStepで済ませておきます。',
          link: { label: 'iOSアクションを読む', href: '/ja/reference/actions/ios' },
        },
        {
          question: '必要なプロトコルに対応していないときは？',
          answer: 'アクションを書けば追加できます。ProbeはアクションとgRPCでやり取りするので、どの言語でも書けます。ビルトインのアクションも同じ手順で接続されています。外部アクションはWorkflowの中でコミットSHAで固定し、Probeはそのコミットのaction.ymlがSHA-256を示す実行ファイルだけを実行します。',
          link: { label: 'アクションについて読む', href: '/ja/guide/concepts/actions' },
        },
        {
          question: '無料ですか？',
          answer: '無料です。ProbeはMIT Licenseのオープンソースです。agentアクションは、選んだモデルの提供元のAPIを自分のAPIキーで呼ぶので、その利用料は提供元から請求されます。',
          link: { label: 'GitHubで見る', href: 'https://github.com/mozership/probe' },
        },
      ],
    },
    copy: { idle: 'コピー', done: 'コピーしました' },
  },
}
