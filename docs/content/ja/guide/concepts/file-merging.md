# ファイルマージ

Probeは複数のファイルから1つのワークフローを組み立てられます。ワークフロー間で設定を共有したり、環境ごとの設定をチェック本体から分離したりするための仕組みです。

## マージの仕組み

ファイルはカンマ区切りの1引数として渡します。

```bash
probe base.yml,production.yml
```

Probeはファイルを**順番に読んで1つのYAMLドキュメントに連結し**、それを解析します。それ以上のことはしません。キーごとの深いマージは行われません。

ここから2つの重要な帰結があります。

1. **複数のファイルで定義されたトップレベルのキーは、最後のファイルの値になります。** `vars:`を再定義するとエントリ単位のマージではなくブロックごと置き換わり、`jobs:`を再定義するとすべてのジョブが置き換わります。
2. **あるファイルで定義したYAMLアンカーを、後続のファイルから参照できます。** 同じドキュメントに入るためです。

パスにはディレクトリやグロブも指定できます。見つかった`.yml`と`.yaml`が、指定した順に連結されます。

```bash
probe common/,workflow.yml
probe "configs/*.yml"
```

## トップレベルのキーで分割する

安全な分割方法は、ファイルごとに異なるトップレベルキーを持たせることです。

**vars.yml:**
```yaml
vars:
  api_url: "{{API_URL ?? 'https://api.example.com'}}"
  timeout: "{{REQUEST_TIMEOUT ?? '30s'}}"
```

**workflow.yml:**
```yaml
name: API Health Check
description: Basic API monitoring

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        User-Agent: "Probe Monitor"
  steps:
    - name: API Health
      uses: http
      with:
        get: /health
      test: res.code == 200
```

```bash
probe vars.yml,workflow.yml
```

## 環境ごとにキーごと差し替える

最後の定義が勝つため、環境ごとのファイルでキーをまるごと差し替えられます。そのファイルにはワークフローが必要とする値をすべて書いてください。書かなかった値は引き継がれず、消えます。

**workflow.yml:**
```yaml
name: API Health Check

jobs:
- name: Health Check
  defaults:
    http:
      url: "{{vars.api_url}}"
  steps:
    - name: API Health
      uses: http
      with:
        get: /health
      test: res.code == 200
```

**production.yml:**
```yaml
vars:
  api_url: https://api.production.example.com
  environment: production
  timeout: 10s
```

```bash
probe workflow.yml,production.yml
```

使われるのは`production.yml`の`vars`ブロックです。`workflow.yml`側にも`vars`があった場合、そちらのエントリは残りません。

## アンカーで値を共有する

ファイルが1つのドキュメントになるため、キーを再定義せずに値を共有したい場合はアンカーを使います。

**shared.yml:**
```yaml
shared:
  json_headers: &json_headers
    content-type: application/json
    accept: application/json
  auth_header: &auth_header
    authorization: "Bearer {{vars.token}}"
```

**workflow.yml:**
```yaml
name: API Test
vars:
  token: "{{API_TOKEN}}"

jobs:
- name: Checks
  defaults:
    http:
      url: "{{vars.api_url}}"
      headers:
        <<: [*json_headers, *auth_header]
  steps:
    - name: List users
      uses: http
      with:
        get: /users
      test: res.code == 200
```

```bash
probe shared.yml,workflow.yml
```

アンカーを定義したファイルを先に置く必要があります。Probeの設定項目にないキー（ここでは`shared`）は無視されます。

## 実践上の注意

- アンカー定義、ワークフロー本体、環境ごとの上書き、の順に並べます
- トップレベルのキーは1ファイルだけが持つようにします。2つのファイルが`vars`を定義してしまうのが、もっともよくある落とし穴です
- `probe dag workflow.yml,production.yml`は実行せずに連結結果を読み込むので、組み合わせが正しいかを手早く確認できます

## 次のステップ

- **[ワークフロー](/ja/guide/concepts/workflows)** - ワークフローの構造
- **[環境管理](/ja/guide/how-tos/environment-management)** - 環境ごとの設定の実践
- **[YAML設定](/ja/reference/yaml-configuration)** - Probeが読むすべてのキー
