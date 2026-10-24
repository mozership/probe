# Agentアクション

Agentアクションは、自然言語で書いたタスクからWebアプリケーションをテストします。テスターと呼ぶモデルが実際のChromeでアプリケーションを操作してタスクを行い、そのあと別に呼び出したモデル、ジャッジが、ページに表示された内容から期待どおりかどうかを判定します。Probeを実行する環境にChromeかChromiumが必要で、モデルはそのプロバイダーのAPIキーで呼び出します。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-agent](https://github.com/mozership/probe-agent)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。Probe v1.21.0以降が必要です。

このアクションは、ページが何をするかも、モデルが何をクリックするかも判定できないため、[ガード](/ja/guide/concepts/guard)に従いません。`--read-only`や`--allow-host`の下では、`--allow-action`で指定しない限り、このアクションを使うステップを拒否します。テスターは`allow_hosts`の範囲にとどめます。ほかのホストのページは開かず、クリックでほかのホストに移ったときはブラウザを戻します。

モードは2つあります。

- **シナリオ**（`task`と`expect`）: テスターがタスクを行い、ジャッジが期待どおりかを判定します。マージ前のように、通るべきことを確かめる用途に向きます。
- **ペルソナ**（`persona`と`goal`）: テスターがペルソナになりきって目標を目指し、途中で気づいた問題を書き留めます。レビュアーが、ペルソナが目標にたどり着けたか、そして書き留めた内容とブラウザの記録のうちどれを指摘とするかを判断します。ステージング環境で繰り返し実行するなど、誰もテストを書いていないところの問題を探す用途に向きます。

決定論的なステップの代わりではありません。ステータスやDBの行、メールのように厳密に書けることはそちらで確かめ、マージの関門にします。agentアクションは、それらでは書けないこと、たとえばユーザーが最後までたどり着けるか、データや設定が積み重なったステージングでもアプリケーションが持ちこたえるかを確かめるためのものです。ペルソナのテストを[`repeat`](/ja/reference/yaml-configuration#repeat)でステージングに対して回せば、人と同じようにアプリケーションを使い続けます。

## 基本的な構文

ワークフローでは、アクションを完全なコミットSHAで固定します。各[リリース](https://github.com/mozership/probe-agent/releases)のノートの先頭に、コピーして使うアクションとコミットがあります。ここでの例では、[`actions`](/ja/reference/yaml-configuration#actions)で一度だけ名前を付けています。これにはProbe v1.24.0以降が必要です。それより前のバージョンでは、`uses`ごとにアクションを省略せずに書きます。

```yaml
name: Sign-up
actions:
  agent: github.com/mozership/probe-agent@4d0d6ccee6165624283b22fa0541b02d17e50ac5 # v0.2.0
secrets:
  - ANTHROPIC_API_KEY
  - TEST_PASSWORD
jobs:
- name: sign-up
  steps:
  - name: A new user signs up
    uses: agent
    with:
      url: https://staging.example.com/
      task: |
        As a new user of this service, sign up with the email address
        {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: |
        The account is created, and the dashboard greets the user.
      secrets:
        EMAIL: "qa+{{random_str(8)}}@example.com"
        PASSWORD: "{{TEST_PASSWORD}}"
    timeout: 5m
    test: res.verdict == "pass"
```

ペルソナのテストでは、`task`と`expect`の代わりに`persona`と`goal`を書きます。

```yaml
  - name: A first-time visitor writes a first note
    uses: agent
    with:
      url: https://staging.example.com/
      persona: |
        Carol, who has never used a notes app and is not technical. She signs
        up with {secret:EMAIL} and the password {secret:PASSWORD}.
      goal: Create an account, and write a first note that says "Buy milk".
      secrets:
        EMAIL: "qa+{{random_str(8)}}@example.com"
        PASSWORD: "{{TEST_PASSWORD}}"
    timeout: 5m
    test: len(filter(res.findings, .severity == "high" && .verified)) == 0
```

タスクやペルソナは日本語で書いても構いません。判定の理由や要約は、タスクやゴールと同じ言語で返ります。

自然言語で書いたテストは決定的ではありません。同じステップでも、実行のたびに違う手順をたどったり、違う判定になったりすることがあります。できるところは、アカウントが作成されたことを`http`や`db`のステップで確かめるなど、決定的なステップをあとに続けて確認してください。また1回の結果を信じるのではなく、[`repeat`](/ja/reference/yaml-configuration#repeat)で複数回の実行のうち何回通ったかを数えてください。

## 仕組み

1. Chromeが`url`を開きます。
2. テスターには`task`と`expect`、そしてブラウザのツールを渡します。テスターはページをrefの付いた要素の木として読み、クリックし、入力し、フォームを埋め、スクリーンショットを撮り、最後に`finish`を呼びます。
3. ジャッジには、タスク、期待、ステップ、ページに表示されたテキスト、最後のページとそのスクリーンショット、コンソールのエラーと失敗したリクエストを渡します。ジャッジはページから引用して`pass`か`fail`を返します。
4. 引用はそれぞれ、ページに表示された内容の中から探します。ページに表示されたものを何も引用していない`pass`は採らず、判定は`blocked`になります。

テスターには判定せずに見たものを説明するよう指示しています。作業をしていない呼び出しが判定を下すためです。

ペルソナのテストでは、テスターは問題に気づいたらすぐに書き留めます。レビュアーはジャッジの代わりに、記録に加えて書き留めた内容と最も遅かったリクエストを受け取ります。記録が裏付ける内容だけを残し、テスター自身の操作ミスによるものは除き、サーバーのエラーのようにテスターが書き留めていないが記録に表れているものを加えます。指摘はそれぞれ記録を引用し、引用が記録に見つかったものは`verified`になります。

## パラメータ

以下は`with`の直下に書きます。

| パラメータ | 型 | デフォルト | 説明 |
|-----------|------|---------|-------------|
| `url` | String | 必須 | ブラウザが最初に開くページ。`http`か`https` |
| `task` | String | シナリオでは必須 | テスターが行うこと |
| `expect` | String | シナリオでは必須 | 終わったあとに成り立っているべきこと |
| `persona` | String | ペルソナでは必須 | テスターが演じる人物。何を知っていて、どう振る舞うか |
| `goal` | String | ペルソナでは必須 | ペルソナが目指すこと |
| `secrets` | Object | なし | モデルに見せない値を名前ごとに指定する。`task`や`persona`には`{secret:NAME}`と書く。[シークレット](#シークレット)を参照 |
| `allow_hosts` | List、または文字列 | `url`のホスト | テスターが開いてよいホスト。`host`、`host:port`、`*.domain`のいずれか。リスト、またはカンマ区切りの文字列 |
| `provider` | String | `anthropic` | `anthropic`、`openai`、`xai`、`gemini`、`sakura`のいずれか。[モデル](#モデル)を参照 |
| `model` | String | `anthropic`では`claude-opus-5-5`、それ以外では必須 | モデルのID |
| `effort` | String | `medium` | `low`、`medium`、`high`、`xhigh`、`max`のいずれか |
| `base_url` | String | プロバイダーのもの | プロバイダーのAPIのURL。プロキシを通すときなどに指定する |
| `vision` | Boolean | `sakura`では`false`、それ以外では`true` | モデルが画像を読めるかどうか。`anthropic`では指定できない |
| `max_steps` | Integer | `40` | テスターがブラウザを呼び出せる回数 |
| `timeout` | 期間の文字列、または秒数 | `4m` | ジャッジを含むテスト全体の制限時間。ステップの`timeout`（指定しなければ`5m`）より短くする |
| `headless` | Boolean | `true` | ウィンドウを表示せずにChromeを動かす |
| `window_w`、`window_h` | Integer | `1280`、`800` | ビューポートの大きさ（ピクセル） |
| `cache` | String | なし | 通ったシナリオテストのステップを保存し、そこから再生するファイル。[リプレイ](#リプレイ)を参照 |
| `cache_mode` | String | `auto` | `auto`、`read`、`strict`、`refresh`のいずれか。[リプレイ](#リプレイ)を参照 |
| `evidence_dir` | String | 一時ディレクトリ | スクリーンショットとレポートを書き出す場所。テストごとにディレクトリを分ける |

## モデル

このアクションはClaude向けに作っています。ClaudeはClaude APIのブラウザツールセットでブラウザを操作するよう学習しています。ほかのプロバイダーのモデルには、同じブラウザのツールをアクション独自のツールとして渡すため、ジャッジ、ペルソナのテスト、リプレイを含めて同じテストを実行できます。タスクをどれだけうまくこなせるかはモデルによります。

APIキーはProbeの環境から読みます。Probeの出力で値を隠すよう、ワークフローの[`secrets`](/ja/reference/yaml-configuration#secrets)に書いてください。

| `provider` | APIキー | 補足 |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` | デフォルト |
| `openai` | `OPENAI_API_KEY` | Responses APIで呼び出し、OpenAI側には何も保存しない。`max`は`xhigh`として送る |
| `xai` | `XAI_API_KEY` | Chat Completionsで呼び出す。`max`は`xhigh`として送る |
| `gemini` | `GEMINI_API_KEY` | Chat Completions互換のAPIで呼び出す。`xhigh`と`max`は`high`として送る |
| `sakura` | `SAKURA_AI_ENGINE_TOKEN` | さくらのAI Engine。Chat Completionsで呼び出す。モデルごとに受け付けるパラメータが違うため`effort`は送らず、`vision`を`true`にしない限り画像も渡さない。`gpt-oss-120b`のようにツールを呼び出せるモデルを選ぶ |

```yaml
secrets:
  - OPENAI_API_KEY
jobs:
- name: sign-up
  steps:
  - name: A new user signs up
    uses: agent
    with:
      provider: openai
      model: gpt-5.4
      url: https://staging.example.com/
      task: Sign up with {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: The dashboard greets the user.
```

画像を読めないモデルは、ページを要素とテキストだけで読みます。スクリーンショットのツールは渡さず、ページ上の座標ではなく要素を操作し、ジャッジにも最後のページを画像として見せません。スクリーンショット自体は証拠として保存します。

## レスポンスオブジェクト

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `res.mode` | String | `scenario`か`persona` |
| `res.verdict` | String | `pass`、`fail`、または判定できなかったときの`blocked`。ペルソナのテストでは、ペルソナが目標にたどり着けたかどうか |
| `res.findings` | List | ペルソナのテストで見つかったアプリケーションの問題。重いものから順に並ぶ。`title`、`severity`（`high`、`medium`、`low`）、`category`（`error`、`functional`、`usability`、`performance`、`content`、`accessibility`）、`expected`、`observed`、`evidence`、`steps`、`url`、`screenshot`、そして根拠のどれかが記録に見つかったかを示す`verified` |
| `res.reason` | String | ジャッジが述べた判定の理由 |
| `res.evidence` | List | ジャッジの引用。`quote`と、それがページに表示されていたかを示す`verified` |
| `res.stopped` | String | テスターの止まり方。`done`、`blocked`、`max_steps`、`timeout`、`no_finish`、`refused`、または`cache_mode: strict`で記録が合わなくなったときの`stale` |
| `res.summary` | String | テスターが行ったこと、見たことの要約 |
| `res.steps` | List | ブラウザの呼び出しごとの`index`、`tool`、`input`、`result`か`error`、`url`、`screenshot`、テスターの`notes` |
| `res.url`、`res.title` | String | テスターが最後にブラウザを置いたページ |
| `res.console_errors` | List | ページがコンソールに書いたエラー |
| `res.network_errors` | List | 失敗したリクエストと、400以上で応答されたリクエスト。`method`、`url`、`status`、`error` |
| `res.cache` | Object | `cache`を指定したときの使われ方。`path`、`mode`、`status`（`miss`、`hit`、`partial`）、再生したステップ数の`replayed`、記録が止まった位置と理由の`diverged_at`と`reason`、アプリケーションが記録と合わなくなって止まったかを示す`stale`、その実行を記録したかを示す`saved` |
| `res.metrics` | Object | 実行どうしを比べるための、モデルに左右されない数値。`steps`、`duration_ms`、`requests`、`failed_requests`、`console_errors`、時間のかかった上位10件のリクエストの`slowest_requests`（`method`、`url`、`status`、`duration_ms`） |
| `res.usage` | Object | モデルへのリクエスト数`requests`と、使ったトークン数 |
| `res.model` | String | モデルのID |
| `res.filepaths` | Object | ステップごとのスクリーンショットを含むMarkdownのレポート`report`、結果のJSON`trace`、最後のページの`screenshot` |
| `res.report` | String | Probeがステップの下に表示する数行 |
| `status` | Integer | `0`。判定はステップの`test`で確かめる |

`blocked`は`fail`と分けています。CAPTCHAやタスクに書かれていない情報のように、アプリケーションの誤りではなく、テスターが先に進めなかったことを表します。

## リプレイ

毎回同じ手順で通るシナリオテストなら、毎回モデルに手順を考えさせる必要はありません。`cache`を指定すると、通った実行のステップをファイルに保存し、次の実行ではモデルを使わずにそれを再生します。ページに表示された内容は引き続き判定するため、ジャッジだけは呼び出します。

```yaml
    with:
      url: https://staging.example.com/
      task: Sign up with {secret:EMAIL} and the password {secret:PASSWORD}.
      expect: The dashboard greets the new user.
      cache: .probe-agent/signup.json
      cache_mode: "{{CACHE_MODE ?? 'auto'}}"
```

リプレイは次のように進みます。

1. 記録が始まったページから始まるときだけ再生します。開始URLがログインなどにリダイレクトすることがあるためです。
2. ステップが操作した要素を、`button "Create account"`のようにロールと名前で探し直します。同じロールと名前の要素がほかにもあった場合はその数も数え、同じ数のうちの同じ順番の要素を選びます。数が違えば曖昧として、推測せずに再生を止めます。
3. ステップのたびに、記録したときと同じルートに着いたこと、記録にないアラートが出ていないことを確かめます。ルートはページのパスとクエリで、IDやトークン、日付のようにレコードごとに発行される値は1つとして扱います。`/orders/42`と`/orders/7`は同じルートで、`/companies`と`/companies?tab=notes`は別のルートです。
4. モデルを使わずに終わるか、止まったところからテスターに引き継ぎます。引き継ぐときは、何を再生し、なぜ止まったかをテスターに伝えます。テストが通れば、その実行をあらためて記録します。

タスクに書かれた値や、名前のようにテスターが考えた値は、再生でもう一度入力します。画面に表示されたコードや日付のように、テスターが画面から読み取った可能性のある値は、その実行に固有の値です。そのステップは値を保存せずに記録し、再生はそこでテスターに引き継ぎます。

記録はタスク、期待、開始URLに結びついていて、どれかが変われば再生しません。シークレットはプレースホルダーのまま、`url`と同じホストのURLはパスとして保存するため、あるホストで作った記録を別のホストで再生できます。記録するのはステップであってモデルではないので、あるモデルで記録したテストを別のモデルで再生できます。記録が変わったときだけ書き込むので、コミットした記録は再生しているあいだ変更されません。ペルソナのテストは毎回違う道をたどるためのものなので、`cache`は指定できません。

| `cache_mode` | 再生 | 記録 | 記録が合わなくなったとき |
|---|---|---|---|
| `auto` | する | 通った実行で記録が変わるとき | テスターが続ける |
| `read` | する | しない | テスターが続ける |
| `strict` | する | しない | モデルを使わずに`blocked`（`stopped: stale`）にする |
| `refresh` | しない | 通った実行 | |

記録ファイルをコミットし、CIでは`strict`で実行してください。記録が合わなくなったとき、誰かが記録し直すまで毎回テスターの費用を払うのではなく、テストを失敗させるためです。記録し直すのは、変更を人が確かめられる場所で`auto`を使って行います。

## シークレット

モデルには、シークレットの場所が`{secret:NAME}`と見えます。モデルがプレースホルダーを入力すると、ブラウザには実際の値を渡します。ページからモデルに見せる内容とアクションが返す内容では、それぞれの値を大文字小文字を問わずプレースホルダーに置き換えて戻します。アプリケーションがメールアドレスを小文字で表示することがあるためです。指定されていないシークレットのプレースホルダーは、入力する文字列ではなく、モデルに伝えるエラーになります。

## CIでの実行

- ランナーにChromeかChromiumをインストールし、`headless: true`のままにします。
- APIキーはCIのシークレットから設定し、ワークフローの`secrets`に書きます。
- `evidence_dir`をアーティファクトとしてアップロードすると、失敗したテストのレポートとスクリーンショットを読めます。
- `cache`を指定したテストは`cache_mode: strict`で実行します。記録が合っているあいだ、モデルには判定だけを頼みます。
