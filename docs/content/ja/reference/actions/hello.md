# Helloアクション

`hello`アクションは成功するだけで何もしないアクションです。プレースホルダーとして、`echo`だけを目的とするステップとして、あるいは式の動作確認に使います。

## 基本的な構文

helloのステップでは`echo`を指定します。`with`に渡した値は`res`から参照できます。

```yaml
steps:
  - name: Report
    uses: hello
    echo: "Checked {{outputs.health.endpoint}}"
```

## パラメータ

このアクション固有のパラメータはありません。`with`に渡した内容はそのまま`res`に返るため、計算した値をoutputsとして公開したいときに便利です。

```yaml
steps:
  - name: Build a summary
    id: summary
    uses: hello
    with:
      run_id: "{{vars.run_id}}"
      checked_at: "{{now().Format('2006-01-02T15:04:05Z07:00')}}"
    outputs:
      run_id: res.run_id
      checked_at: res.checked_at
```

## レスポンスオブジェクト

このステップは常に成功し、渡した内容がそのまま返ります。`test`のないステップもテストなしとは表示されずに成功となり、`probe check`も警告しません。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.<key>` | 任意 | `with`に渡したキーがそのまま入ります |
| `res.status` | Integer | 常に`0` |
| `res.dump` | Boolean | 常に`false` |
| `rt.duration` | String | アクションにかかった時間（例: `"15µs"`） |
| `rt.sec` | Float | アクションにかかった時間（秒） |
| `status` | Integer | 常に`0` |

`status`と`dump`はアクションが設定するため、`with`に同じ名前のキーを渡しても、その値は返りません。

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - ステップのプロパティ
