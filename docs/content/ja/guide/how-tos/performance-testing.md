# パフォーマンステスト

ステップはそれぞれ、アクションにかかった時間を記録します。
テストでは、その時間に上限を設けられます。
このガイドでは、1回の応答に上限を設ける方法、同じ操作から複数の標本を取る方法、負荷試験用のツールをワークフローから実行する方法を説明します。
Probeは負荷試験用のツールではありません。
標本を取るために低い一定のレートでリクエストを送ることはできますが、それ以上の負荷をかけない理由は最後の節で述べます。

## ステップが測る時間

各ステップは、アクションにかかった時間を`rt`として公開します。

| フィールド | 型 | 説明 |
|-------|------|-------------|
| `rt.duration` | String | `"120ms"`のような文字列表記 |
| `rt.sec` | Float | 同じ時間の秒数 |

どこからどこまでを測るかはアクションごとに異なります。
各ステップはアクションを別々のプロセスで実行するため、接続はステップをまたいで再利用されません。
`http`アクションは、リクエストを送り始めてから応答のヘッダーを受け取るまでを測ります。
接続と、`https`ならTLSハンドシェイクの時間は含まれ、ボディを読む時間は含まれません。
`db`アクションは、接続を開いてから最後の行を読むまでを測ります。
`shell`アクションは、コマンドの開始から終了までを測ります。
時間を比べるのは、同じアクションで測ったもの同士に限ってください。

## 1回の応答への上限

`rt.sec`は、応答のほかの値と同じようにテストで上限を設けられます。

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search
  steps:
  - name: Search by keyword
    id: search
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.5
    outputs:
      elapsed_ms: rt.sec * 1000
```

1回のリクエストは1つの標本にすぎません。
キャッシュが冷えていた、ガベージコレクションが走った、ネットワークが混んでいたといった理由で、直前の何倍もかかるリクエストはよくあります。
そのため、普段の時間に近い上限を置くと、コードに原因がなくてもときどき失敗します。
上限は普段の時間より十分に大きく取り、インデックスを失ったクエリのように操作を何倍も遅くする変更を検出する用途に使います。
数割遅くなったかどうかを見るには、複数の標本を取ります。

## 同じ操作の複数の標本

`repeat`はジョブを繰り返し実行し、各ステップのテストは実行のたびに評価されます。

```yaml
name: Search Latency
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Search
  repeat:
    count: 50
    interval: 100ms
  steps:
  - name: Search by keyword
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.3
```

結果には、テストが成り立った実行の回数が表示されます。

```
⏺ Search (Completed in 6.42s)
  ⎿ 0. Search by keyword (repeating 50 times)
        ✓  50/50 success (100.0%)
```

テストが成り立たなかった実行が1回でもあればジョブは失敗します。
つまり、ここでの`rt.sec`のテストも、すべての標本が上限に収まることを求めています。
繰り返して得られるのは回数です。
1回のリクエストでは成否しかわかりませんが、`48/50 success (96.0%)`からは、大半のリクエストが上限に収まり、どれだけの割合が収まらなかったかがわかります。

既定では実行は1回ずつ順に進み、`interval`は1回の実行が終わってから次を始めるまでの待ち時間です。
`async: true`にすると、前の実行が終わったかどうかに関係なく`interval`ごとに次の実行を始めます。
互いの応答を待たないクライアントと同じように、一定のレートでリクエストが送られます。

```yaml
jobs:
- name: Search at 10 requests a second
  repeat:
    count: 100
    interval: 100ms
    async: true
  steps:
  - name: Search by keyword
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /search?q=probe
    test: res.code == 200 && rt.sec < 0.3
```

前の実行を待つ場合は、ほかにリクエストのないサービスで各リクエストを測ることになります。
一定のレートで送る場合は、遅い応答があっても後続のリクエストの送信は遅れません。
そのため、サービスの処理が追いつかなくなると、後続のリクエストの時間にそれが表れます。
レートは低く保ってください。
理由は[負荷試験](#負荷試験)で述べます。

## エンドポイントの裏のクエリ

エンドポイントが遅い原因は、そこで実行されるクエリにあることがよくあります。
`db`アクションはそのクエリを単独で実行するので、時間がデータベースでかかっているかどうかを切り分けられます。

```yaml
name: Order History
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"
  db_dsn: "{{DB_DSN}}"

jobs:
- name: Order history of one user
  steps:
  - name: Endpoint
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /users/42/orders
    test: res.code == 200 && rt.sec < 1
  - name: Query behind it
    uses: db
    with:
      dsn: "{{vars.db_dsn}}"
      query: SELECT * FROM orders WHERE user_id = ? ORDER BY created_at DESC LIMIT 50
      params: [42]
    test: res.code == 0 && rt.sec < 0.2
```

`?`はMySQLとSQLiteのプレースホルダーです。
PostgreSQLでは`$1`と書きます。

2つの時間は同じものを測っていないので、一方を他方と比べるのではなく、それぞれのステップに上限を設けます。
`http`ステップの時間は、クライアントから見たリクエスト全体の時間です。
`db`ステップの時間はクエリだけの時間に、接続の時間を加えたものです。
ステップは毎回接続し直しますが、サービスはすでに持っている接続でクエリを実行します。
数ミリ秒で終わるクエリなら、時間のほとんどは接続の時間です。
クエリが上限に収まっているのにエンドポイントが上限を超えるなら、時間はサービスの別の箇所でかかっています。

## 日単位で見る応答時間

1回の実行では、サービスが日を追って遅くなっていくことはわかりません。
cronやCIで毎日上限を確かめれば、それがわかります。
スケジュールでワークフローを実行する方法は[モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)で説明しています。
終了ステータスで、上限が守られたかどうかがスケジューラーに伝わります。
レポートのファイルは、同じパスに書き出す次の実行で置き換えられます。
各回の記録を残すには、`--report json=reports/$(date +%F).json`のように実行ごとに別のパスを指定します。

## 負荷試験

Probeは、組み込みのものも含め、ステップごとにアクションのプロセスを起動します。
各実行のテンプレート、テスト、outputsは、プロセスを起動する側で評価します。
1回のリクエストを送るステップでも、Probe自身の処理に数十ミリ秒かかります。
`repeat`の回数は、`PROBE_MAX_REPEAT_COUNT`で上限を引き上げない限り10000回未満です。
Probeは、サービスが普通にさばけるレートで自身の限界に達してしまいます。
しかも記録された時間からは、限界に達したのがProbeとサービスのどちらなのかがわかりません。
負荷試験には、[k6](https://k6.io/)のように、多数のリクエストを同時に送って時間を記録するために作られたツールが必要です。

ワークフローでは、そのツールをステップとして実行し、その前後でサービスを確かめられます。

```yaml
name: Checkout under Load
vars:
  api_url: "{{API_URL ?? 'http://localhost:8080'}}"

jobs:
- name: Checkout under load
  steps:
  - name: Service is up
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /health
    test: res.code == 200
  - name: Load with k6
    uses: shell
    timeout: 15m
    with:
      cmd: k6 run --env API_URL="$API_URL" load/checkout.js
      env:
        API_URL: "{{vars.api_url}}"
    test: res.code == 0
  - name: Orders were all written
    uses: http
    with:
      url: "{{vars.api_url}}"
      get: /orders/count
    test: res.code == 200 && res.body.failed == 0
```

k6は、スクリプトのしきい値のどれかが成り立たないと`0`以外のステータスで終了するので、そのステップは失敗します。
前後のステップでは、負荷がデータに何をもたらしたかを確かめます。
これはk6からは確かめられないことです。
しきい値を満たせなかったときこそデータの状態を知る必要があるので、後のステップはk6の成否にかかわらず実行します。
URLはコマンドに書き込まず、コマンドの環境変数としてk6に渡しています。
`API_URL`にどのような値が入っても、コマンドそのものは変わりません。

## 次のステップ

- **[モニタリングワークフロー](/ja/guide/how-tos/monitoring-workflows)** - ワークフローをスケジュールで実行する
- **[環境管理](/ja/guide/how-tos/environment-management)** - 同じワークフローを別の環境に向ける
- **[エラーハンドリング戦略](/ja/guide/how-tos/error-handling-strategies)** - リトライ、タイムアウト、ステップが失敗したときの対処
