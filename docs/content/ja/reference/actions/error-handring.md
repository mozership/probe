# アクションエラーハンドリング

ステップは`test`が偽になったとき、またはアクション自体がエラーを返したときに失敗します。失敗しても同じジョブの残りのステップは実行され、ジョブは失敗扱いになります。そのジョブを`needs`に指定したジョブは、`always: true`がない限りスキップされます。ワークフローの終了ステータスは、`test`の失敗なら`1`、アクションのエラーなら`3`になります。

失敗を無視するための設定はありません。ワークフローを失敗させたくないチェックは、`test`で判定せず結果をoutputsとして記録します。

```yaml
steps:
  - name: Required check
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/health"
    test: res.code == 200

  - name: Optional check
    id: optional
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/experimental"
    outputs:
      available: res.code == 200
      detail: 'res.code >= 400 ? res.status : ""'

  - name: Report
    uses: hello
    echo: "Experimental endpoint: {{outputs.optional.available ? 'available' : outputs.optional.detail}}"
```

## 一時的な失敗とハング

一時的な失敗には`retry`、応答が返らないおそれのあるステップには`timeout`を使います。

```yaml
  - name: Flaky endpoint
    uses: http
    timeout: 10s
    retry:
      max_attempts: 3
      interval: 2s
    with:
      method: GET
      url: "{{vars.api_url}}/flaky"
    test: res.code == 200
```

`retry`のパラメータは`max_attempts`（必須、1以上）、`interval`、`initial_delay`です。`max_attempts`の上限は環境変数`PROBE_MAX_ATTEMPTS`（既定10000）で変更できます。

## 後続の分岐

ジョブが失敗すると、それを`needs`に指定したジョブはスキップされます。失敗したときにも動かしたいジョブには`always: true`を付け、結果をoutputsとして公開して、そのジョブの`skipif`で値を評価します。リクエストが接続できなかったステップはoutputsを公開しないため、`??`で既定値を与えます。

```yaml
name: Health
vars:
  api_url: "{{API_URL}}"
jobs:
- id: health-check
  name: Health Check
  steps:
    - name: Check
      id: health
      uses: http
      with:
        method: GET
        url: "{{vars.api_url}}/health"
      test: res.code == 200
      outputs:
        healthy: res.code == 200

- name: Alert
  needs: [health-check]
  always: true
  skipif: outputs.health?.healthy ?? false
  steps:
    - name: Notify
      uses: hello
      echo: "{{vars.api_url}} is not healthy"
```

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - `retry`と`timeout`のプロパティ
