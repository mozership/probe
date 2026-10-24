# CLI基本

Probeコマンドライン・インターフェース（CLI）は、Probeと対話する主要な方法です。このガイドでは、マスターする必要があるすべての基本的なコマンド、オプション、技術をカバーします。

## 基本的な使用法

Probeを実行する最も基本的な方法は：

```bash
$ probe workflow.yml
```

これは`workflow.yml`で定義されたワークフローを実行し、結果を表示します。

### コマンド構文

オプションを先に書き、その後にワークフローファイルを指定します。

```bash
$ probe [options] <workflow-file>
```

- **`workflow-file`**: YAMLワークフローファイルへのパス（必須）
- **`options`**: 動作を変更するさまざまなフラグ（オプション）

## オプション

Probe実行時のオプションを説明します。

### ヘルプと情報

利用可能なオプションについてのヘルプを取得：

```bash
$ probe --help
# あるいは -h
```

インストールされたバージョンを確認：

```bash
$ probe --version
```

### 詳細出力

詳細なログを有効にして、内部で何が起こっているかを確認：

```bash
$ probe workflow.yml --verbose
# あるいは -v
```

詳細モードでは以下が表示されます：
- 詳細なHTTPリクエスト/レスポンス情報
- ステップ実行タイミング
- 変数解決の詳細
- アクションのログ

**詳細出力の例**（`…`はここで省いた部分です）：
```
[DEBUG] --- Step 0: Check Homepage
[DEBUG] Request:
[DEBUG]   body: ""
[DEBUG]   headers:
[DEBUG]     Accept: */*
[DEBUG]     User-Agent: probe-http/1.0.0
[DEBUG]   method: GET
[DEBUG]   timeout: 30s
[DEBUG]   url: https://example.com/
[DEBUG] Response:
[DEBUG]   body: |
[DEBUG]     <!doctype html><html lang=en><head><meta charset=utf-8>…
[DEBUG]   code: 200
[DEBUG]   cookies: {}
[DEBUG]   filepath: ""
[DEBUG]   headers:
[DEBUG]     Content-Type: text/html; charset=utf-8
[DEBUG]     …
[DEBUG]   status: 200 OK
[DEBUG] RT: 245ms
[DEBUG] Test: Success (input: res.code == 200)
[DEBUG] Env:
[DEBUG]   outputs: {}
[DEBUG]   repeat_index: 0
[DEBUG]   report: ""
[DEBUG]   rt:
[DEBUG]     duration: 245ms
[DEBUG]     sec: 0.245
[DEBUG]   status: 0
[DEBUG]   vars: {}
```

### タイミング情報表示

開始時刻とレスポンス時間を表示：

```bash
$ probe workflow.yml --timing
```

これにより、`--verbose`の完全な詳細さなしにタイミング情報が出力に追加されます。

### レポート出力モード

ジョブ実行中にレポートをどう出力するかを制御します。

```bash
probe --output stream workflow.yml
```

- `auto`（デフォルト）: 対話的な端末ではspinner、それ以外ではstream
- `spinner`: spinnerを表示し、実行完了後にレポート全体を出力
- `stream`: ジョブが確定するたびにそのブロックをstdoutに出力し、実行中は
  ステップの進捗をstderrにログ出力

CIではspinnerが描画されず、ワークフロー完了まで何も出力されないため、
`stream`が適しています。ジョブブロックは宣言順で先行するジョブがすべて出力
されるまで保留されるため、stdoutのレポートは`spinner`の場合と同一です。
ステップの進捗行はstderrに出るので、stdoutをリダイレクトすれば整形された
レポートだけが得られます。

モードは環境変数`PROBE_OUTPUT`でも指定できます。フラグのほうが優先されます。

### レポートファイル

端末のレポートは人が読むためのものです。CI、スクリプト、あとからの確認のために
結果を残したい場合は、ファイルにも書き出します。

```bash
probe --report junit,markdown workflow.yml
```

- `json`: 失敗したステップのリクエストとレスポンスを含む実行全体を
  `probe-report.json`に書き出す
- `junit`: CIがテスト結果として表示できるJUnit XMLを`probe-junit.xml`に書き出す
- `markdown`: 要約、ジョブの表、失敗したステップごとの節を`probe-report.md`に
  書き出す
- `github-summary`: 同じページをGitHub Actionsのジョブサマリーに追記する。
  GitHub Actionsの外では警告を出して飛ばす

`--report junit=out/junit.xml`のように`=`のあとにパスを書くと、別の場所に
書き出せます。ファイルはすべてのジョブが終わってから書き出され、端末の
レポートは変わりません。形式の並びは環境変数`PROBE_REPORT`でも指定でき、
フラグのほうが優先されます。

### オプションの組み合わせ

複数のオプションを組み合わせることができます：

```bash
$ probe -v --timing workflow.yml
```

## 複数ファイルマージ

Probeの強力な機能の一つは、複数のYAMLファイルをマージできることです：

```bash
$ probe base.yml,overrides.yml
```

### ファイルマージの使用例

**1. 環境固有の設定:**

**base-workflow.yml:**
```yaml
name: API Health Check
jobs:
- name: API
  defaults:
    http:
      url: *endpoint
  steps:
  - name: Check API
    uses: http
    with:
      get: /foo/bar
    test: res.code == 200
```

**production.yml:**
```yaml
# 本番環境固有の設定
x_alias:
  x_endpoint: &endpoint "https://api.production.example.com"
```

**staging.yml:**
```yaml
# ステージング環境固有の設定
x_alias:
  x_endpoint: &endpoint "https://api.staging.example.com"
```

異なる環境で実行：
```bash
# 本番環境
$ probe base-workflow.yml,production.yml

# ステージング環境
$ probe base-workflow.yml,staging.yml
```

**2. 共有設定:**

**common-config.yml:**
```yaml
# 共有するヘッダー。ワークフローからアンカーで参照する
shared:
  common_headers: &common_headers
    User-Agent: "Probe Health Check v1.0"
```

**api-check.yml:**
```yaml
name: API Monitoring
jobs:
- name: API checks
  defaults:
    http:
      headers:
        <<: *common_headers
  steps:
    # ... ステップ定義
```

```bash
$ probe common-config.yml,api-check.yml
```

**3. 特定の値をオーバーライド:**

```bash
$ probe workflow.yml,local-overrides.yml
```

マージ順序が重要です - 後のファイルが前のファイルの値を上書きします。

## 環境変数の使用

Probeはworkflow wideの`vars`オブジェクトを使用して環境変数を変数に設定することができます：

```yaml
name: Database Test

vars:
  db_host: "{{DB_HOST ?? 'localhost'}}"
  db_pass: "{{DB_PASS ?? 'secret!!!'}}"

jobs:
- name: DB Operations
  defaults:
    db:
      dsn: mysql://foobar:{{vars.db_pass}}@{{vars.db_host}}:3306/probetest
  steps:
  - name: Test MySQL Connection
    uses: db
    with:
      query: SELECT 1 as connection_test, NOW() as server_time
    test: res.code == 0
```

実行前に環境変数を設定：

```bash
export DB_HOST="https://db.example.com"
export DB_PASS="****************"
$ probe workflow.yml
```

## 終了コード

終了コードで、何が起きたかを区別できます。

- **`0`**: 成功 - すべてのジョブが正常に完了
- **`1`**: テストが失敗した
- **`2`**: ファイルが無い、YAMLが不正、未知のフラグなど、ワークフローやコマンドラインが誤っている
- **`3`**: 接続の拒否やタイムアウトなど、アクションがエラーを返した

1回の実行で複数の種類の失敗が起きた場合は、`3`より`2`、`1`より`3`を優先します。

これにより、ProbeはスクリプトやCI/CDパイプラインでの使用に最適です：

```bash
#!/bin/bash
if probe health-check.yml; then
    echo "✅ Health check passed"
    deploy_application
else
    echo "❌ Health check failed"
    exit 1
fi
```

## 実用的な例

CI、定期的な監視、手元での開発、負荷の確認のいずれも同じバイナリで行います。異なるのは呼び出し方だけです。

### 1. CI/CD統合

パイプラインは終了コードで処理を分岐します。

```bash
# CI/CDパイプラインで
probe smoke-tests.yml
if [ $? -eq 0 ]; then
    echo "Smoke tests passed, proceeding with deployment"
else
    echo "Smoke tests failed, aborting deployment"
    exit 1
fi
```

### 2. Cronジョブ監視

同じワークフローを定期的に実行すれば監視になります。

```bash
# crontabで - 5分ごとに実行
*/5 * * * * /usr/local/bin/probe /opt/monitoring/health-check.yml >> /var/log/probe.log 2>&1
```

### 3. 開発テスト

開発中は手元の設定ファイルを共通のファイルの後ろに指定してマージし、`-v`で各ステップを確認します。

```bash
# 開発中のクイックテスト
$ probe -v api-tests.yml,local-config.yml
```

## 出力の解釈

Probeの出力を理解することで、問題を迅速に特定できます：

### 成功した実行

ジョブごとに、含まれるステップと所要時間が出力されます。

```
My Health Check
Monitoring application health

⏺ Frontend Check (Completed in 0.45s)
  ⎿ 0. ✔︎  Check Homepage (234ms)
     1. ✔︎  Check API Health (156ms)

Total workflow time: 0.45s ✔︎ All jobs succeeded
```

**主要な指標:**
- **⏺**: ジョブ完了
- **✔︎**: ステップ成功
- **緑色のテキスト**: 成功ステータス
- **合計時間**: 全体の実行時間

### 失敗した実行

失敗したステップには印が付き、成立しなかったテストが併記されます。

```
My Health Check
Monitoring application health

⏺ Frontend Check (Failed in 1.23s)
  ⎿ 0. ✘  Check Homepage (1.23s)
              failed: res.code == 200
                res.code = 500
              request:
                method: GET
                url: https://example.com
              response:
                body: Internal Server Error
                code: 500
     1. ⏭  Check API Health (skipped)

Total workflow time: 1.23s ✘ 1 job(s) failed
```

**主要な指標:**
- **✘**: ステップ失敗
- **⏭**: ステップスキップ（前の失敗による）
- **赤色のテキスト**: 失敗ステータス
- **Request/Response**: 失敗したHTTPリクエストのデバッグ情報

### 部分的成功

ジョブごとに結果が出るため、1つのジョブの失敗で他の成功が見えなくなることはありません。

```
Multi-Service Check
Checking multiple services

⏺ Critical Services (Completed in 0.67s)
  ⎿ 0. ✔︎  Database Check (234ms)
     1. ✔︎  API Check (156ms)

⏺ Optional Services (Failed in 2.34s)
  ⎿ 0. ✘  External API Check (2.34s)

Total workflow time: 2.34s ✘ 1 job(s) failed
```

## よくある問題のトラブルシューティング

コマンドラインで起きる問題の多くは、ワークフローの内容ではなくファイルに起因します。ファイルの場所、パースできるか、アクセスできるかです。

### ファイルが見つからない

ファイルを指定していないか、指定したパスが存在しない場合に出ます。

```
[ERROR] workflow is required
```

**解決策:** 有効なファイルパスを提供していることを確認：
```bash
$ probe ./workflows/health-check.yml
```

### YAML構文エラー

メッセージには、パーサーが処理を止めた行が示されます。

```
[ERROR] yaml: line 5: mapping values are not allowed in this context
```

**解決策:** YAML構文を確認：
- インデントにはタブではなくスペースを使用
- コロンを使った適切なキー・バリュー構文を確認
- 特殊文字を含む文字列はクォートで囲む

### 権限エラー

ファイルは存在するものの、実行中のユーザーが読めない状態です。

```
[ERROR] permission denied
```

**解決策:** ワークフローファイルが読み取り可能であることを確認：
```bash
chmod +r workflow.yml
```

### ネットワークタイムアウト

ステップがハングまたはタイムアウトする場合：
- `--verbose`を使用して詳細なネットワーク情報を確認
- ターゲットURLへのネットワーク接続を確認
- HTTPアクションでタイムアウト値の追加を検討

## ベストプラクティス

以下では、コマンドラインを短く保ち、実行を再現できる状態にするための、ワークフローファイルの命名と配置を扱います。

### 1. 説明的なファイル名を使用

ファイル名はcronの設定やCIのログに現れます。そのため、何を確認するワークフローかがわかる名前にします。

```bash
# 良い
$ probe api-health-check.yml
$ probe database-migration-test.yml

# そうでもない
$ probe test.yml
$ probe workflow.yml
```

### 2. ディレクトリで整理

目的ごとにまとめておけば、マージ時の引数が短くなります。

```bash
# 目的別にワークフローを整理
$ probe monitoring/health-check.yml
$ probe deployment/smoke-tests.yml
$ probe maintenance/cleanup.yml
```

### 3. 設定ファイルを使用

シークレットと環境固有の値は別ファイルに保管：

```bash
$ probe workflow.yml,configs/production.yml
```

### 4. 本番環境前に検証

常に詳細モードで最初にテスト：

```bash
# 徹底的にテスト
$ probe -v new-workflow.yml,test-config.yml

# 本番環境にデプロイ
$ probe new-workflow.yml,production-config.yml
```

## 次のステップ

CLI基本をマスターしたので、次の内容に進むことができます：

1. **[クイックスタート](/ja/guide/introduction/quickstart)** - 5分で最初のワークフローを作成・実行
2. **[最初のワークフロー](/ja/guide/introduction/your-first-workflow)** - 簡単な例から始める
3. **[リファレンスを確認](/ja/reference/actions/variables)** - 利用可能なすべてのオプションを深く理解

Probeの機能はすべてCLIから使います。これらの基本をマスターしたので、高度な監視と自動化ワークフローを構築する準備が整いました。
