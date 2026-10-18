# アクションエラーハンドリング

ステップは`test`が偽になったとき、またはアクション自体がエラーを返したときに失敗します。失敗しても同じジョブの残りのステップは実行され、ジョブは失敗扱いになります。そのジョブを`needs`に指定したジョブはスキップされ、ワークフローの終了ステータスは`test`の失敗なら`1`、アクションのエラーなら`3`になります。

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

他のジョブが失敗したかどうかで分岐する手段はありません。ジョブが失敗すると後続はスキップされるためです。結果に応じて処理を変えたい場合は、結果をoutputsとして公開し、後続のジョブの`skipif`でその値を評価します。

```yaml
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
      outputs:
        healthy: res.code == 200

- name: Alert
  needs: [health-check]
  skipif: outputs.health.healthy
  steps:
    - name: Notify
      uses: hello
      echo: "{{vars.api_url}} is not healthy"
```

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - `retry`と`timeout`のプロパティ
