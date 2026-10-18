# シェルアクション

`shell`アクションはシェルコマンドとスクリプトを安全に実行し、包括的な出力キャプチャとエラーハンドリングを提供します。

## 基本的な構文

シェルのステップに必要なのはコマンドだけで、他のパラメータには既定値があります。

```yaml
steps:
  - name: "Execute Build Script"
    uses: shell
    with:
      cmd: "npm run build"
    test: res.code == 0
```

## パラメータ

シェルステップに必要なのはコマンドで、加えてインタプリタ、作業ディレクトリ、環境変数、実行時間の上限を指定できます。

### `cmd` (必須)

**型:** String  
**説明:** 実行するシェルコマンド  
**サポート:** テンプレート式

```yaml
vars:
  api_url: "{{API_URL}}"

with:
  cmd: "echo 'Hello World'"
  cmd: "npm run {{vars.build_script}}"
  cmd: "curl -f {{vars.api_url}}/health"
```

### `shell` (オプション)

**型:** String  
**デフォルト:** `/bin/sh`  
**許可値:** `/bin/sh`, `/bin/bash`, `/bin/zsh`, `/bin/dash`, `/usr/bin/sh`, `/usr/bin/bash`, `/usr/bin/zsh`, `/usr/bin/dash`

```yaml
with:
  cmd: "echo $0"
  shell: "/bin/bash"
```

### `workdir` (オプション)

**型:** String  
**説明:** コマンド実行用の作業ディレクトリ  
**サポート:** テンプレート式

```yaml
with:
  cmd: "pwd && ls -la"
  workdir: "/app/src"
  workdir: "{{vars.project_path}}"
```

相対パスは、ワークフローファイルのディレクトリではなく、Probeを実行したディレクトリを基準に解決されます。ディレクトリは存在している必要があります。

### `timeout` (オプション)

**型:** StringまたはDuration  
**デフォルト:** `30s`  
**形式:** Go duration形式 (`30s`, `5m`, `1h`) または数値 (秒)

```yaml
with:
  cmd: "npm test"
  timeout: "10m"
  timeout: "300"  # 300秒
```

`timeout`を過ぎても終わらないコマンドは停止しますが、ステップにはテストできる結果が残ります。`res.timed_out`が`true`、`res.code`が`-1`になり、`res.stdout`と`res.stderr`にはそれまでに書いた内容が入ります。コマンドがバックグラウンドで起動したプロセスは待ちません。コマンドが終了するか停止された後、出力を読むのは最大1秒です。

### `env` (オプション)

**型:** Object  
**説明:** コマンドに設定する環境変数  
**サポート:** 値でのテンプレート式

```yaml
vars:
  production_api_url: "{{PRODUCTION_API_URL}}"

with:
  cmd: "npm run build"
  env:
    NODE_ENV: "production"
    API_URL: "{{vars.production_api_url}}"
    BUILD_VERSION: "{{vars.version}}"
```

数値や真偽値は文字列として渡すため、`PORT: 8080`は`PORT`を`8080`に設定します。これらの変数は、Probe自身が動いている環境に追加されます。

### `background` (オプション)

**型:** Boolean  
**デフォルト:** `false`  
**説明:** コマンドを起動し、終了を待たずに次へ進む

```yaml
with:
  cmd: "python3 -m http.server 8080 --bind 127.0.0.1"
  background: true
```

backgroundは、テスト対象のサーバーのように、後続のステップが動いていることを前提とするコマンドに使います。ステップはコマンドを起動した時点で返るため、`res.code`は`-1`になり、`res.stdout`と`res.stderr`は空です。`ready`を指定しない限り、`timeout`は適用されません。

- **出力:** 標準出力と標準エラー出力は、実行ごとに作られる1つのログファイルに書き込まれ、そのパスは`res.log`に入ります。同じコマンドを2回起動すれば、ファイルも2つになります。
- **寿命:** コマンドはステップやジョブが終わっても動き続けるため、後続のジョブのステップからも使えます。ワークフローが終わると、Probeはコマンドとそこから起動されたプロセスに`SIGTERM`を送り、3秒後に残っていれば`SIGKILL`を送り、ログファイルを削除します。ログを後で使うなら、ステップの中で読んでください。
- **中断:** Ctrl+Cで中断した場合や、`SIGTERM`か`SIGHUP`を受け取った場合も、Probeは終了する前に同じ手順でコマンドを止めます。2回目のCtrl+CでProbeはすぐに終了します。`SIGKILL`などでProbe自身が強制終了した場合だけ、コマンドとログファイルが残ります。

[embedded](/ja/reference/actions/embedded)のジョブの中で起動したコマンドは、そのジョブが終わった時点で止まります。

### `ready` (オプション)

**型:** Object  
**説明:** backgroundのコマンドが、準備ができたときに書く文字列`log`を書くまで、ステップを返さずに待つ

```yaml
- name: Start the server
  uses: shell
  with:
    cmd: python3 -u -m http.server 8080 --bind 127.0.0.1
    background: true
    ready:
      log: Serving HTTP
  test: status == -1

- name: Use it at once
  uses: http
  with:
    get: http://127.0.0.1:8080/
  test: res.code == 200
```

`ready`がなければ、backgroundのコマンドの後のステップで、たいていリクエストをリトライして、準備ができたかを確かめる必要があります。コマンドが起動に失敗すると、リトライを使い切るだけで理由はわかりません。`ready`を指定するとコマンドを起動したステップの中で待ち、失敗したときはコマンドが書いた内容を添えてすぐに失敗します。

| 先に起きたこと | `status` | `res.code` | `res.timed_out` | `res.stdout` |
|---|---|---|---|---|
| コマンドが標準出力か標準エラー出力に文字列を書いた | `-1` | `-1` | `false` | 空（ほかのbackgroundのコマンドと同じ） |
| コマンドが終了し、起動したプロセスも残っていない | `1` | 終了コード | `false` | コマンドが書いた内容 |
| `timeout`を過ぎた | `1` | `-1` | `true` | コマンドが書いた内容 |

- **待ち方:** 文字列は、コマンドが書くそばからログの中で探します。パターンではなく文字列として比べます。待つ時間の上限は`timeout`で、指定しなければ`30s`です。それまでに準備ができなかったコマンドは、ワークフローが止めるのと同じ手順で止めます。ステップをリトライしたときに、前回のコマンドがポートを使ったまま残らないようにするためです。
- **シェルが先に終わる場合:** `server &`のようなコマンドでは、シェルが終わっても起動したプロセスは動き続けます。そのプロセスが文字列を書くかもしれないため、起動したものがシェルのプロセスグループに残っている間は待ち続けます。自分でセッションを作るデーモンのようにグループを離れたプロセスは追いません。グループに残るプロセスがなくなった時点でコマンドは終了したものとして扱い、そのプロセスもほかと一緒には止めません。そうしたプログラムは、`--foreground`や`-f`のようなオプションがあれば、フォアグラウンドで動かしてください。
- **出力のバッファリング:** Pythonのように、端末以外に書くときに出力をためるプログラムは、文字列をすぐには書かないことがあります。`python3 -u`のように、すぐ書くようにしてください。

backgroundでないコマンドに指定した場合、`ready`がマップでない場合、`log`以外のキーがある場合、`log`がない、空、または文字列でない場合は、コマンドを実行する前にエラーになります。数値は`log: "8080"`のようにクォートしてください。

## リトライ機能

shellアクションは統一されたステップレベルのリトライ機能をサポートしています。これにより、一時的な障害やサービス起動時間に対してコマンドを自動的に再実行できます。

```yaml
- name: "Wait for Service Startup"
  uses: shell
  with:
    cmd: "curl -f http://localhost:8080/health"
  retry:
    max_attempts: 30      # 最大試行回数
    interval: "2s"        # リトライ間隔
    initial_delay: "5s"   # 初回実行前の待機時間（オプション）
  test: res.code == 0
```

リトライするかどうかは`test`の結果で決まり、`test`が真になった時点で終わります。`test`のないステップはリトライしません。リトライ機能の詳細については[アクションガイド](/ja/guide/concepts/actions#リトライ機能)を参照してください。

## レスポンス形式

結果には終了コードと、標準出力および標準エラー出力が入ります。

```yaml
res:
  code: 0                    # 終了コード (0 = 成功)
  stdout: "Build successful" # 標準出力
  stderr: ""                 # 標準エラー出力
  pid: 12345                 # シェルのプロセスID
  timed_out: false           # timeoutで停止したときにtrue

req:
  cmd: "npm run build"       # 元のコマンド
  shell: "/bin/sh"          # 使用されたシェル
  workdir: "/app"           # 作業ディレクトリ
  timeout: "30s"            # タイムアウト設定
  env:                      # 環境変数
    NODE_ENV: "production"
  background: false         # background設定
```

`background: true`の場合、結果ができた時点でコマンドはまだ動いています。

```yaml
res:
  code: -1                   # まだ終了していない
  stdout: ""                 # 空 (出力はログに書かれる)
  stderr: ""
  pid: 12345                 # シェルのプロセスID
  log: "/tmp/probe-shell-action.1234567890.log" # 標準出力と標準エラー出力のログファイル
```

`stdout`全体が前後の空白を除いて1つのJSONオブジェクトまたは配列の場合は、`res.json`にもデコードした値が入ります。JSONを出力するコマンドの結果を、式ごとに`parse_json(res.stdout)`と書かずにレスポンスボディと同じように読めます。`res.stdout`にはテキストがそのまま残ります。

```yaml
- name: Find the email
  id: found
  uses: shell
  with:
    cmd: ./driver find-emails -subject 'inbound'   # {"count":1,"emails":[{"id":"M1"}]} を出力する
  test: res.code == 0 && res.json.count == 1
  outputs:
    id: res.json.emails[0].id
```

それ以外の場合、つまりプレーンテキスト、`42`のようなスカラー値、JSON Lines、正しくないJSONでは`res.json`は設定されず、nilとして読まれます。backgroundのコマンドには`stdout`がないため`res.json`もありません。ログファイルを`parse_json`で読んでください。

`res`と`req`のほかに、テストでは次の値も使えます。

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `status` | Integer | 終了コードが`0`なら`0`、それ以外は`1`、backgroundのコマンドは`-1` |
| `rt.duration` | String | コマンドにかかった時間。`"4.7ms"`など |
| `rt.sec` | Float | 同じ時間を秒で表した値 |

## 使用例

以下では単一のコマンドから始めて、ビルドとテスト、環境ごとのデプロイ、失敗内容の報告までをつないだ例を示します。

### 基本的なコマンド実行

コマンドを1つ実行し、その終了コードを検証します。

```yaml
- name: "System Information"
  uses: shell
  with:
    cmd: "uname -a"
  test: res.code == 0
```

### ビルドとテストパイプライン

段階ごとにステップを分けておけば、失敗したときにどの段階かがわかります。

```yaml
- name: "Install Dependencies"
  uses: shell
  with:
    cmd: "npm ci"
    workdir: "/app"
    timeout: "5m"
  test: res.code == 0

- name: "Run Tests"
  uses: shell
  with:
    cmd: "npm test"
    workdir: "/app"
    env:
      NODE_ENV: "test"
      CI: "true"
  test: res.code == 0 && res.stdout contains "All tests passed"
```

### 環境固有のデプロイ

コマンド自体を変数から組み立てられます。1つのステップで異なる環境へデプロイするにはこの方法を使います。

```yaml
vars:
  target_env: "{{TARGET_ENV}}"
  deploy_key: "{{DEPLOY_KEY}}"
```

```yaml
- name: "Deploy to Environment"
  uses: shell
  with:
    cmd: "./deploy.sh {{vars.target_env}}"
    workdir: "/deploy"
    shell: "/bin/bash"
    timeout: "15m"
    env:
      DEPLOY_KEY: "{{vars.deploy_key}}"
      TARGET_ENV: "{{vars.target_env}}"
  test: res.code == 0
```

### 後続のステップのためのサーバー

backgroundで起動したサーバーは、後続のステップのために動き続けます。次のステップはサーバーが応答するまでリトライし、最後のステップはProbeがログを削除する前にサーバーのログを読みます。

```yaml
- name: "Start the Server"
  id: server
  uses: shell
  with:
    cmd: "python3 -m http.server 8080 --bind 127.0.0.1"
    background: true
  test: res.code == -1 && res.pid > 0
  outputs:
    log: res.log

- name: "Wait Until It Answers"
  uses: http
  with:
    url: "http://127.0.0.1:8080"
    get: "/"
  retry:
    max_attempts: 20
    interval: "500ms"
  test: res.code == 200

- name: "Show the Server Log"
  uses: shell
  with:
    cmd: "cat {{outputs.server.log}}"
  test: res.code == 0
```

### エラーハンドリングとデバッグ

終了コードだけでは足りない場合、テストから`res.stdout`と`res.stderr`を参照します。

```yaml
- name: "Service Health Check"
  uses: shell
  with:
    cmd: "curl -sS http://localhost:8080/health"
  test: res.code == 0 && res.stdout contains "ok" && res.stderr == ""

- name: "Debug Failed Build"
  uses: shell
  with:
    cmd: "npm run build:debug"
  # デバッグ出力をキャプチャするために失敗を許可
  outputs:
    debug_info: res.stderr
```

### サービス起動とリトライ

起動を待つ場合は、条件が満たされるまでステップ自体を繰り返します。

```yaml
- name: "Wait for Database Startup"
  uses: shell
  with:
    cmd: "pg_isready -h postgres -p 5432"
  retry:
    max_attempts: 30
    interval: "2s"
    initial_delay: "10s"
  test: res.code == 0

- name: "Verify API Health"
  uses: shell
  with:
    cmd: |
      # APIエンドポイントの確認
      curl -f -H "Accept: application/json" \
           http://api:8080/health
  retry:
    max_attempts: 60
    interval: "1s"
  test: res.code == 0

- name: "Wait for Build Artifact"
  uses: shell
  with:
    cmd: |
      # ビルド成果物の確認
      test -f ./dist/app.js && \
      test -s ./dist/app.js
  retry:
    max_attempts: 20
    interval: "500ms"
  test: res.code == 0
```


## セキュリティ機能

シェルアクションはいくつかのセキュリティ対策を実装しています：

- **シェルパス制限**: 承認されたシェル実行ファイルのみを許可
- **作業ディレクトリ検証**: 存在しないディレクトリを拒否
- **タイムアウト保護**: `timeout`を超えて動くコマンドを停止

コマンドはProbe自身の環境変数に`env`を加えた環境で動き、出力はそのまま返されます。

## エラーハンドリング

一般的な終了コードとその意味：

- **0**: 成功
- **1**: 一般的なエラー
- **2**: シェル組み込みコマンドの誤用
- **126**: コマンドを実行できない（権限拒否）
- **127**: コマンドが見つからない
- **130**: Ctrl+Cでスクリプトが終了
- **255**: 終了ステータスが範囲外

```yaml
- name: "Handle Different Exit Codes"
  uses: shell
  with:
    cmd: "some_command_that_might_fail"
  test: |
    res.code == 0 ? true :
    res.code == 127 ? res.stderr contains "not found" :
    res.code < 128
```
