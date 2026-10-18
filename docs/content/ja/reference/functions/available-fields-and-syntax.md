# 利用可能なフィールドと構文

Probeの式は [expr-lang/expr](https://expr-lang.org/)（v1.17）で評価されます。
このリファレンスに載っている関数は、exprの組み込み関数か、Probeが独自に登録した関数のどちらかです。

式には2つの形があります。

- **テンプレート式** — 文字列中の <span v-pre>`{{ ... }}`</span> を評価結果で置き換えます
- **ブール式** — 式そのものを条件として評価します

## 利用可能なフィールド

レベル   | フィールド名 | テンプレート式 | ブール式 | 補足
---:     | ---:         | :---:          | :---:    | ---
workflow | name         | -              | -        | ワークフロー名
workflow | description  | -              | -        | ワークフロー説明
workflow | vars         | ✅             | -        | グローバル変数。環境変数は変数名そのままで参照します
job      | name         | ✅             | -        | ジョブ名
job      | skipif       | -              | ✅       | ジョブをスキップする条件式
step     | name         | ✅             | -        | ステップ名
step     | with         | ✅             | -        | アクション引数
step     | test         | -              | ✅       | アサーション
step     | echo         | ✅             | -        | レポート出力
step     | vars         | ✅             | -        | ステップ変数
step     | outputs      | ✅             | -        | ワークフロー間共有変数
step     | skipif       | -              | ✅       | ステップをスキップする条件式
step     | iter         | -              | -        | イテレーション変数

## 関数の構文

関数は直接呼び出すか、パイプ演算子（`|`）でつなぎます。
パイプは左辺の値を**第1引数**として渡します。

```yaml
vars:
  service: "{{SERVICE_NAME | upper}}"
  slug: "{{replace(lower(SERVICE_NAME), ' ', '-')}}"
  path: "{{BASE_URL | trimSuffix('/')}}/api"
```

## 演算子

四則演算や比較は関数ではなく演算子で書きます。
`add()`や`mod()`のような関数はありません。

`+` `-` `*` `/` `%` `**` `==` `!=` `<` `>` `&&` `||` `!` `??`（nil合体）`in` `contains` `startsWith` `endsWith` `matches`（正規表現）

```yaml
test: |
  res.code == 200 &&
  res.headers["Content-Type"] contains "application/json" &&
  rt.sec < 1
```

## 評価の制限

式の評価には安全のための上限があります。

- 式の長さは最大1000000文字
- 評価のタイムアウトは5秒
- `parse_json`、`encode_base64`、`decode_base64`、`template`は1000000文字を超える引数を、`file`は1000000バイトを超えるファイルを受け付けません
- `template`の入れ子は10段まで。1回の結果は1000000文字まで、1つの呼び出しの中で入れ子になった展開の合計は10000000文字まで

## 関数カテゴリ

- **[Probe固有関数](/ja/reference/functions/probe)** - Probeが独自に登録する関数
- **[文字列関数](/ja/reference/functions/string)** - 文字列操作とフォーマット
- **[数値関数](/ja/reference/functions/mathematics)** - 数値演算と型変換
- **[日時関数](/ja/reference/functions/datetime)** - 日付と時刻のユーティリティ
- **[配列・マップ関数](/ja/reference/functions/array)** - コレクション操作
- **[JSON・エンコーディング関数](/ja/reference/functions/json)** - JSONとBase64
- **[高度な関数使用法](/ja/reference/functions/advanced-usage)** - 組み合わせとベストプラクティス
