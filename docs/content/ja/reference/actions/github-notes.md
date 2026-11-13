# GitHub Notesアクション

GitHub Notesアクションは、実行の[ノート](/ja/reference/yaml-configuration#notes)をGitHubに投稿します。投稿先は、プルリクエストのコメント、GitHub Actionsのジョブのサマリー、Issueのいずれかです。

アクションは、ステップが通ったかどうかとは別に、人に向けたノートを残せます。後続のステップはそれを`notes`として読み、各ノートにはジョブとステップが付き、シークレットは隠されています。Probeはノートをどこにも投稿しません。投稿するのはこのアクションで、どのタイミングでどのノートを投稿するかはワークフローに書きます。このアクションはノートを投稿するためだけのもので、GitHub全般を操作するアクションではありません。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-github-notes](https://github.com/mozership/probe-github-notes)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。`notes`コンテキストを持つ最初のバージョンであるProbe v1.32.0以降が必要です。

## 基本的な構文

ステップでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-github-notes/releases)のノートの先頭に、コピーして使う`uses`の行があります。このステップは最後のジョブに置き、そのジョブは[`always`](/ja/reference/yaml-configuration#always)付きでほかのジョブを[`needs`](/ja/reference/yaml-configuration#needs)にします。こうすると、ほかのジョブがどう終わっても、すべて終わったあとに実行され、すべてのノートを読めます。

```yaml
name: Shop
actions:
  github-notes: github.com/mozership/probe-github-notes@76735813d682751a3b743a0dbce0ad49a8d372ad # v0.1.0
secrets:
  - GITHUB_TOKEN
vars:
  github_token: "{{GITHUB_TOKEN}}"
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

  - name: Notes
    needs: [checkout]
    always: true
    steps:
      - name: Put the notes on the pull request
        uses: github-notes
        with:
          to: comment
          token: "{{vars.github_token}}"
          notes: "{{ notes }}"
        test: status == 0
```

## ノートの投稿先

`to`で書き込む先を選びます。

| `to` | 書き込む先 | 次の実行では | ノートがないとき |
|---|---|---|---|
| `comment` | プルリクエストのコメント | 同じコメントを編集する | コメントがあれば、ノートがないことを書いて編集する。新しくは作らない |
| `summary` | GitHub Actionsのジョブのサマリー | ジョブの実行ごとに別のサマリーになる | 何も書かない |
| `issue` | `labels`が付いたopenなIssue | openの間は同じIssueを編集する | openなIssueがあれば、ノートがないことを書いて編集し、クローズする。次にノートが出た実行で新しく作る |

コメントは、変更に対して実行するワークフローで、レビューする人に読んでもらうためのものです。サマリーはトークンが要らず、コメントするプルリクエストがない実行や、レビューする人に向けないノートに使います。Issueは、ステージング環境に対して毎晩実行するような定期実行のワークフローで、ノートを作業として追跡するためのものです。実行がノートを残す間はopenのままで、ノートを残さなかった最初の実行でクローズされます。

アクションは`key`ごとにコメントかopenなIssueを1つ持ち、本文の先頭に`<!-- probe-github-notes:<key> -->`というマーカーを書きます。プルリクエストのコメント、または`labels`が付いたopenなIssueから、マーカーで始まる最初のものを探して編集し、なければ新しく作ります。ノートの一部を別のコメントに投稿するには、別の`key`を使います。

```yaml fragment
      - name: The critical notes, in a comment of their own
        uses: github-notes
        with:
          to: comment
          key: critical
          token: "{{vars.github_token}}"
          notes: "{{ filter(notes, .level == 'critical') }}"
```

同じ場所に投稿する2つの実行が同時に走ると、どちらもコメントを見つけられずにそれぞれ作ることがあり、以降の実行は最初の1つだけを編集します。別々の実行の間をアクションが調整することはできないので、GitHub Actionsの`concurrency`などで、ワークフローの実行が重ならないようにしてください。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|----------|---------|-------------|
| `to` | String | はい | - | `comment`、`summary`、`issue`のいずれか |
| `notes` | Array | はい | - | `"{{ notes }}"`のような、`notes`コンテキストのエントリ。空のリストは、ノートを残さなかった実行として扱う |
| `body` | String | いいえ | - | ノートの上に書くMarkdown |
| `timeout` | Duration | いいえ | `30s` | ステップの制限時間。`10s`のような形式か秒数で指定する |
| `token` | String | `comment`、`issue` | - | リポジトリにコメントやIssueを書けるトークン |
| `repository` | String | いいえ | `GITHUB_REPOSITORY` | `owner/name`形式のリポジトリ |
| `key` | String | いいえ | `probe` | アクションが編集するコメントやIssueの名前。英数字、`.`、`_`、`-`を使える |
| `api_url` | String | いいえ | `GITHUB_API_URL`、なければ`https://api.github.com` | REST API。GitHub Enterprise Serverで使う |
| `number` | Integer | いいえ | イベントのプルリクエスト | `comment`で、コメントするプルリクエストまたはIssue |
| `title` | String | いいえ | `Probe notes` | `issue`のタイトル |
| `labels` | Array | いいえ | `[probe]` | `issue`に付けるラベル。アクションはこのラベルでIssueを探す |

ほかの`to`のためのパラメータは読まないので、ジョブの[`defaults`](/ja/reference/yaml-configuration#defaults)で、どの`to`のステップにも`token`を渡せます。どれにも当てはまらないキーは、ステップを失敗させます。`action.yml`がこれらを`params`として宣言しているので、`probe check`はそのようなキーを行番号付きで報告します。

`notes`は、`notes`コンテキストと同じ形のエントリを受け取ります。`level`が`info`、`warning`、`critical`のいずれでもないエントリや、`body`のないエントリがあると、ステップは失敗します。手で書いたリストではなく、コンテキストそのもの、または`filter(notes, .level != 'info')`のようにその一部を渡してください。

`to: comment`で`number`がないときは、GitHub Actionsの実行を起動したイベントのプルリクエストにコメントします。イベントは`GITHUB_EVENT_PATH`から読み、`pull_request`と`pull_request_target`が対象です。ブランチへのpushなどそれ以外のイベントでは、何も投稿せずにステップを通し、`res.posted`はfalseになります。

GitHub Actionsの実行のトークン`GITHUB_TOKEN`には、書き込みの権限が必要です。プルリクエストへのコメントには`pull-requests: write`、IssueやIssueへのコメントには`issues: write`を付けます。Probeが出力の中で隠すように、トークンは[`secrets`](/ja/reference/yaml-configuration#secrets)に入れてください。

## 書き込む内容

先頭の1行でレベルごとのノートの件数を数え、そのあとにジョブとステップの名前の下にノートを並べます。ステップにはその状態と、`repeat`のジョブであれば何回目の実行かを添えます。ノートはレベルに応じたGitHubのアラートで書き、criticalは`CAUTION`、warningは`WARNING`、infoは`NOTE`です。タイトルと本文はそのまま書きます。

```markdown
The run left 2 notes: 1 critical, 1 info.

### Checkout

**A first-time shopper** · passed

> [!CAUTION]
> **The checkout button is under the cookie banner**
>
> On a 390px wide screen the banner covers the button until it is closed.

> [!NOTE]
> The search took 4s to show results.

<sub>From [run 123456](https://github.com/owner/name/actions/runs/123456) of _Shop_.</sub>
```

最後の行はGitHub Actionsの実行へのリンクで、GitHub Actionsの外では書きません。Probeが隠した値は、ノートの中では`<secret:NAME>`や`<redacted>`になっています。GitHubはこれをHTMLタグとみなして表示しないので、アクションはコードの外ではこれを文字として書きます。コードの中では、そのまま表示されます。

GitHubが受け付けるのは、コメントとIssueでは65536文字まで、ステップのサマリーでは1 MiBまでで、サマリーはそれまでに書かれた分も含みます。これより長いページでは末尾のノートを省き、省いた件数を1行で書きます。ノートをすべて省いても収まらないほど`body`が長いときは、ステップは失敗します。

## レスポンスオブジェクト

| プロパティ | 型 | 説明 |
|----------|------|-------------|
| `res.posted` | Boolean | アクションが何かを書いたかどうか |
| `res.action` | String | `created`、`updated`、`closed`（Issue）、`written`（サマリー）、`none`のいずれか |
| `res.number` | Integer | 投稿したプルリクエストかIssue、または投稿するものがなかったプルリクエスト。対象がないときとサマリーでは`0` |
| `res.url` | String | GitHub上のコメントかIssue。サマリーでは空 |
| `res.id` | Integer | コメントかIssueのID |
| `res.code` | Integer | GitHubへの最後のリクエストのステータスコード。サマリーでは`0` |
| `req` | Object | `to`、`notes`の件数、投稿先。トークンは含まない |
| `rt` | Duration | ステップにかかった時間 |
| `status` | Integer | `0` |

書き込めないトークンでの403や、存在しないプルリクエストへの404など、GitHubがエラーを返すと、ステップはGitHubのステータスとメッセージで失敗します。書き込みをGitHubが301や302でリダイレクトした場合も失敗します。そのまま従うと、書き込みが読み取りとして送り直されるためです。

アクションは、GitHubに投稿したノートを`res.notes`として返しません。返すと、後続のステップに同じノートがもう一度残るためです。

## ガードの下での動作

このアクションは実行のガードに従い、`action.yml`で`guard: [read-only, allow-host]`を宣言しています。そのため、どちらのガードの下でも`--allow-action`なしで実行します。拒否したステップは、リクエストを送る前に種類`refused`で失敗します。

- `--read-only`の下では、`to: comment`と`to: issue`はGitHubに書き込むので拒否します。`to: summary`はジョブのページにだけ書くので実行します。
- `--allow-host`の下では、`api_url`のホストと、各リダイレクト先のホストが、実行が許可するものでなければなりません。GitHubであれば`api.github.com`です。ポートのないURLは、スキームの既定のポートとして扱います。

[ガード](/ja/guide/concepts/guard)を参照してください。

## 関連項目

- **[ノート](/ja/reference/yaml-configuration#notes)** - アクションがノートを残し、後続のステップがそれを読む仕組み
- **[外部アクション](/ja/guide/concepts/actions#外部アクション)** - Probeが外部アクションを解決し、照合する仕組み
