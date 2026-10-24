# 高度な関数使用法

このページでは、関数の組み合わせ方と書き方のコツをまとめます。

## 関数チェーン

パイプ演算子（`|`）は左辺の値を第1引数として渡します。

```yaml
vars:
  clean_name: "{{RAW_NAME | trim | lower | replace(' ', '-')}}"
  api_base: "{{BASE_URL | trimSuffix('/')}}"

with:
  url: "{{vars.api_base}}/api"
```

同じことは入れ子の関数呼び出しでも書けます。
チェーンが3段を超えるようなら、`vars`に分けたほうが読みやすくなります。

```yaml
vars:
  clean_name: "{{replace(lower(trim(RAW_NAME)), ' ', '-')}}"
```

## 条件式での利用

`test`や`skipif`はブール式なので、テンプレートの波カッコは不要です。

```yaml
test: |
  res.code == 200 &&
  len(res.body.items) > 0 &&
  upper(res.body.status) contains "SUCCESS"

skipif: vars.environment != "production"
```

## デフォルト値とフォールバック

`??`演算子がnil合体演算子です。
`default()`や`coalesce()`という関数はないので、`??`をつなげます。

```yaml
vars:
  timeout: "{{REQUEST_TIMEOUT ?? '30s'}}"
  api_url: "{{CUSTOM_API_URL ?? DEFAULT_API_URL ?? 'https://api.example.com'}}"

outputs:
  item_count: len(res.body.items ?? [])
  user_name: upper(res.body.user?.name ?? 'unknown')
```

## データの整形

配列・マップ関数を組み合わせると、レスポンスから必要な値だけを取り出せます。
`outputs`の値は式なので、テンプレートの波カッコで囲みません。

```yaml
outputs:
  # 有効なユーザー名をカンマ区切りで並べる
  active_users: "join(map(filter(res.body.users, #.active), #.name), ', ')"

  # 成功率をパーセントで計算する
  success_rate: 'string(round(res.body.successful * 100 / res.body.total)) + "%"'

  # ステータスごとの件数をまとめる
  by_status: "toJSON(groupBy(res.body.items, #.status))"
```

## 値は一度だけ計算する

ワークフローの`vars`は実行開始時に一度だけ評価されます。
複数のステップで同じ値を使うなら、`vars`に置いて参照します。

```yaml
vars:
  api_url: "{{BASE_URL | trimSuffix('/')}}"
  run_id: "{{random_str(8)}}"

jobs:
- name: test
  steps:
  - name: Health check
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/health"
      headers:
        X-Run-Id: "{{vars.run_id}}"
    test: res.code == 200

  - name: Metrics
    uses: http
    with:
      method: GET
      url: "{{vars.api_url}}/metrics"
      headers:
        X-Run-Id: "{{vars.run_id}}"
    test: res.code == 200
```

ステップごとに`random_str(8)`を書くと、ステップごとに別の値になります。
同じ実行を識別するIDが欲しい場合は、上のように`vars`で固定してください。

## 関連項目

- **[Probe固有関数](/ja/reference/functions/probe)** - Probeが登録する関数
- **[YAML設定](/ja/reference/yaml-configuration)** - 式が使えるフィールド
- **[概念: 式とテンプレート](/ja/guide/concepts/expressions-and-templates)** - 式言語のガイド
