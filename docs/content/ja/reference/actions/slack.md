# Slackアクション

Slackアクションは、Slackのチャンネルにメッセージを投稿します。投稿するのは、実行の[ノート](/ja/reference/yaml-configuration#notes)、ワークフローに書いたテキスト、またはその両方です。

アクションは、ステップが通ったかどうかとは別に、人に向けたノートを残せます。後続のステップはそれを`notes`として読み、各ノートにはジョブとステップが付き、シークレットは隠されています。Probeはノートをどこにも投稿しません。このアクションはノートをSlackに投稿するので、criticalのノートや定期実行の失敗を、チームが普段やりとりしている場所で知らせられます。Slackに対して行うのはメッセージの投稿だけです。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-slack](https://github.com/mozership/probe-slack)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。`notes`コンテキストを持つ最初のバージョンであるProbe v1.32.0以降が必要です。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-slack/releases)のノートの先頭に、コピーして使う`uses`の行があります。このステップは最後のジョブに置き、そのジョブは[`always`](/ja/reference/yaml-configuration#always)付きでほかのジョブを[`needs`](/ja/reference/yaml-configuration#needs)にします。こうすると、ほかのジョブがどう終わっても、すべて終わったあとに実行され、すべてのノートを読めます。

```yaml
name: Nightly
actions:
  slack: github.com/mozership/probe-slack@06717bb03b81a793fa2276d2837ef881e52e093c # v0.1.1
secrets:
  - SLACK_WEBHOOK_URL
vars:
  slack_webhook: "{{SLACK_WEBHOOK_URL}}"
jobs:
  # helloは受け取ったwithをそのままresとして返すので、
  # ノートを残すアクションの代わりに使える
  - name: Checkout
    id: checkout
    steps:
      - name: A first-time shopper
        uses: hello
        with:
          notes:
            - level: critical
              title: The checkout button is under the cookie banner
              body: On a 390px wide screen the banner covers the button until it is closed.
            - level: info
              body: The search took 4s to show results.

  - name: Tell the team
    needs: [checkout]
    always: true
    steps:
      - name: Post the critical notes
        uses: slack
        with:
          webhook_url: "{{vars.slack_webhook}}"
          text: Critical notes of the nightly run
          notes: "{{ filter(notes, .level == 'critical') }}"
        test: status == 0
```

## 2つの投稿方法

| | `webhook_url` | `token`と`channel` |
|---|---|---|
| 仕組み | [Incoming Webhook](https://api.slack.com/messaging/webhooks) | `chat:write`を持つボットトークンで[`chat.postMessage`](https://api.slack.com/methods/chat.postMessage)を呼ぶ |
| 投稿先のチャンネル | Webhookを作ったときのチャンネル | ボットが招待されたチャンネルのうち、`channel`にIDか`#name`で指定したもの |
| 返す値 | `res.posted` | `res.posted`と、メッセージの`res.channel`、`res.ts` |

Webhookはほかに何も要らないので、1つのチャンネルに投稿するならこちらです。トークンを使うと、ステップごとに指定したチャンネルに投稿できます。どちらか一方だけを指定し、WebhookのURLかトークンを[`secrets`](/ja/reference/yaml-configuration#secrets)に入れてください。WebhookのURLを知っていれば、誰でもそのWebhookに投稿できます。

```yaml fragment
      - name: The critical notes, to the channel of the team on call
        uses: slack
        with:
          token: "{{vars.slack_token}}"
          channel: "#oncall"
          notes: "{{ filter(notes, .level == 'critical') }}"
```

実行のたびに新しいメッセージを投稿します。メッセージの編集やスレッドへの返信はしません。pushのたびに最新に保つメッセージは、[github-notes](/ja/reference/actions/github-notes)でプルリクエストに投稿してください。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|----------|---------|-------------|
| `webhook_url` | String | 2つの方法のどちらか | - | Incoming WebhookのURL |
| `token` | String | 2つの方法のどちらか | - | `chat:write`を持つボットトークン |
| `channel` | String | `token`のとき | - | `C0123456789`のようなIDか、`#name`で指定するチャンネル |
| `text` | String | `text`か`notes` | - | ノートの上に書くMarkdown。単独でも投稿できる |
| `notes` | Array | `text`か`notes` | - | `"{{ notes }}"`のような、`notes`コンテキストのエントリ |
| `api_url` | String | いいえ | `https://slack.com/api` | `token`で使うWeb API |
| `timeout` | Duration | いいえ | `30s` | ステップの制限時間。`10s`のような形式か秒数で指定する |

どれにも当てはまらないキーは、ステップを失敗させます。`action.yml`がこれらを`params`として宣言しているので、`probe check`はそのようなキーを行番号付きで報告します。

`notes`が空で`text`もないときは、何も投稿せず、`res.posted`はfalseになります。ノートがなかったことを毎晩伝えるメッセージは、読む人にとって邪魔になるだけだからです。ノートがなくても`text`があれば、それだけを投稿します。定期実行が失敗したことを伝えるときなどに使います。

`notes`は、`notes`コンテキストと同じ形のエントリを受け取ります。`level`が`info`、`warning`、`critical`のいずれでもないエントリや、`body`のないエントリがあると、ステップは失敗します。手で書いたリストではなく、コンテキストそのもの、または`filter(notes, .level == 'critical')`のようにその一部を渡してください。

## 投稿する内容

メッセージはBlock Kitの[`markdown`](https://api.slack.com/reference/block-kit/blocks#markdown)ブロックで組み立てるので、ノートのMarkdownは書いたとおりに表示されます。先頭に`text`、次にレベルごとのノートの件数を数える1行、そのあとにノートごとのブロックが続きます。各ブロックには、レベルの絵文字、タイトル、本文と、ジョブ、ステップ、ステップの状態、`repeat`のジョブであれば何回目の実行かを示す1行が入ります。上のワークフローは次のメッセージを投稿します。

```markdown
Critical notes of the nightly run

**The run left 1 note: 1 critical.**

🚨 **critical · The checkout button is under the cookie banner**

On a 390px wide screen the banner covers the button until it is closed.

_Checkout › A first-time shopper · passed_
```

メッセージのプレーンテキストにも同じ内容が入ります。`text`、件数の1行、表示した各ノートのレベル、タイトル、ジョブ、ステップ、本文です。Slackはこれを通知に表示し、スクリーンリーダーはブロックの代わりにこれを読み上げます。

Probeが隠した値は、ノートの中では`<secret:NAME>`や`<redacted>`になっています。Slackはこれをリンクやメンションとみなすので、アクションはこれをコードとして書きます。コードの中では、そのまま表示されます。

Slackが受け付けるのは、1つのメッセージにつき50ブロックまでで、`markdown`ブロックの合計は12000文字までです。これより長いメッセージでは末尾のノートを省き、省いた件数をブロックで書きます。ノートをすべて省いても収まらないほど`text`が長いときは、ステップは失敗します。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `res.posted` | Boolean | メッセージを投稿したかどうか |
| `res.channel` | String | `token`のとき、メッセージのチャンネル。Webhookでは空 |
| `res.ts` | String | `token`のとき、Web APIでメッセージを指すタイムスタンプ。Webhookでは空 |
| `res.code` | Integer | レスポンスのステータスコード。何も投稿しなかったときは`0` |
| `req` | Object | `via`（`webhook`か`token`）、トークンのときは`channel`と`api_url`、`text`、`notes`の件数。WebhookのURLとトークンは含まない |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | `0` |

Slackがエラーを返すと、ステップはSlackの応答で失敗します。たとえば`chat.postMessage`の`channel_not_found`や`not_in_channel`、削除されたWebhookの404 `no_service`、待つべき秒数付きの429です。投稿をSlackが301や302でリダイレクトした場合も失敗します。そのまま従うと、投稿がGETとして送り直されるためです。エラーにはWebhookのURLも、リダイレクト先のパスも含めず、ホストだけを書きます。

アクションは、投稿したノートを`res.notes`として返しません。返すと、後続のステップに同じノートがもう一度残るためです。

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、どちらのガードの下でも`--allow-action`なしで実行します。拒否したステップは、リクエストを送る前に種類`refused`で失敗します。

- `--read-only`の下では、投稿は書き込みなので、すべてのステップを拒否します。
- `--allow-host`の下では、`webhook_url`か`api_url`のホストと、各リダイレクト先のホストが、実行が許可するものでなければなりません。Webhookであれば`hooks.slack.com`、トークンであれば`slack.com`です。ポートのないURLは、スキームの既定のポートとして扱います。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[ノート](/ja/reference/yaml-configuration#notes)** - アクションがノートを残し、後続のステップがそれを読む仕組み
- **[GitHub Notes](/ja/reference/actions/github-notes)** - ノートをプルリクエスト、ジョブのサマリー、Issueに投稿する
- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
