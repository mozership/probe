# Embeddedアクション

`embedded`アクションは**ジョブファイル**を1つのステップとして実行します。共通のセットアップやチェックを別ファイルに切り出し、複数のワークフローから再利用できます。

読み込むファイルはワークフローではなくジョブです。`name`、`steps`、必要なら`defaults`を持ち、`jobs`キーはありません。

## 基本的な構文

**auth.yml:**
```yaml
name: Authentication
steps:
  - name: Get token
    id: get_token
    uses: shell
    with:
      cmd: echo "0123456789"
    test: res.code == 0
    outputs:
      mytoken: replace(res.stdout, '\n', '')
```

**workflow.yml:**
```yaml
jobs:
- name: Main
  steps:
    - name: Authenticate
      id: auth
      uses: embedded
      with:
        path: "./auth.yml"
        vars:
          environment: "{{vars.environment}}"
      test: res.code == 0
      outputs:
        token: res.outputs.mytoken

    - name: Call the API
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/me"
        headers:
          authorization: "Bearer {{outputs.auth.token}}"
      test: res.code == 200
```

## パラメータ

埋め込みのステップでは、実行するジョブファイルと、そこに渡す変数を指定します。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `path` | String | 必須 | - | ジョブファイルのパス。ワークフローファイルからではなく、カレントディレクトリからの相対パスとして解決されます |
| `vars` | Object | 任意 | `{}` | 埋め込みジョブに渡す変数。埋め込みジョブの中では`vars.<name>`で参照します |

ジョブファイルの中で`uses: ./greet`のようにローカルの[外部アクション](/ja/guide/concepts/actions#外部アクション)を使うと、ワークフローの中でワークフローファイルから探すのと同じく、そのジョブファイルからの相対パスで探します。


### 名前を付けた外部アクション

ジョブファイルのステップでは、ワークフローが[`actions`](/ja/reference/yaml-configuration#actions)で外部アクションに付けた名前を、`uses`と`defaults`のキーに書けます。ジョブファイル自身は`actions`を持ちません。使える名前は、そのジョブを埋め込むステップがあるワークフローのものです。

**workflow.yml:**
```yaml
actions:
  redis: github.com/mozership/probe-redis@7d60e0699a914e3c987ed5f2403ed8a7f3d176fa # v0.1.0

jobs:
- name: Session
  steps:
    - name: Check the session store
      uses: embedded
      with:
        path: "./jobs/session.yml"
      test: res.code == 0
```

**jobs/session.yml:**
```yaml
name: Session store
defaults:
  redis:
    url: redis://localhost:6379
steps:
  - name: The store answers
    uses: redis
    with:
      commands: [PING]
    test: res.results[0] == "PONG"
```

- コミットはワークフローに1度書くだけで、埋め込むジョブファイルにも効きます。2つのワークフローから埋め込まれるジョブファイルは、それぞれのワークフローが名前を付けたアクションで動きます。
- ジョブファイルがさらに埋め込むジョブファイルも、同じワークフローの名前で読まれます。
- `greet: ./greet`のようにローカルパスを指す名前は、ジョブファイルがどこにあっても、ワークフローファイルの隣のディレクトリを指します。
- [ガード](/ja/guide/concepts/guard)の下では、`--allow-action`には名前ではなくアクションを完全な形で渡します。ローカルパスを指す名前の場合は、そのディレクトリの絶対パスです。
- ワークフローが付けていない名前はアクションではありません。最初のジョブの前に終了ステータス2で実行が止まり、どのジョブファイルのどのステップがその名前を使っているかを示します。ステップが埋め込むジョブファイルと、それがさらに埋め込むジョブファイルが対象です。`outputs`から読む`path`のように実行が進まないと分からない場合は、代わりにジョブを埋め込むステップが同じ内容で失敗します。`probe check`は、ジョブを埋め込むステップの`path`の行でその名前を報告します。`path`がテンプレートのときは報告しません。

## レスポンスオブジェクト

埋め込みジョブの終了後、`res`にその結果と出力が入ります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.code` | Integer | 埋め込みジョブのすべてのステップが成功したとき`0` |
| `res.outputs` | Object | 埋め込みジョブのステップが公開したoutputs。出力名がキーになります |
| `res.report` | String | 埋め込みジョブのレポート。親のレポートにもネストして表示されます |
| `res.error` | String | 失敗時のエラーメッセージ |
| `res.dump` | Boolean | 常に`false` |
| `status` | Integer | `res.code`と同じ値 |
| `rt` | Object | 埋め込みジョブの実行時間 |

`res.outputs`のキーは出力名なので、`mytoken`として公開した値は`res.outputs.mytoken`で読みます。同じ値は公開したステップのidの下にも`res.outputs.get_token.mytoken`として入っています。2つのステップが同じ名前を公開した場合、`res.outputs.<name>`には先に公開したステップの値が入り、ステップのidの下の形にはそれぞれの値が入ります。

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - ステップのプロパティ
- **[ファイルマージ](/ja/guide/concepts/file-merging)** - 複数ファイルからワークフローを組み立てる
