# iOSアクション

iOSアクションは、iOSシミュレータ上のアプリをXCUITestで操作します。アプリの起動、タップ、文字の入力、要素の待機、テキストの読み取り、スクリーンショットの保存ができます。Xcodeが必要なので、macOSでだけ動きます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-ios](https://github.com/mozership/probe-ios)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。[実行の終了後にアクションを呼び出す](/ja/guide/concepts/actions#実行の後始末)Probe v1.27.0以降が必要です。

## 仕組み

アクションは、XCUITestのテストランナー（以下、ランナー）を通してアプリを操作します。ランナーは、ステップが初めて必要としたときに、そのMacのXcodeでソースからビルドし、キャッシュに置きます。実行（run）の中でアクションを使う最初のステップが、`xcodebuild test-without-building`でランナーをシミュレータ上に起動します。以後、各アクションは`127.0.0.1`へのHTTPでランナーに送られ、ランナーがXCUITestで要素を探して操作します。ランナーは同じ実行の後続のステップでも使われ、実行が終わると停止します。

| 必要なもの | 用意の仕方 |
|------|-----|
| Xcode | `xcode-select -p`がCommand Line ToolsではなくXcodeを指している必要があります：`sudo xcode-select -s /Applications/Xcode.app` |
| iOSシミュレータのランタイム | `xcodebuild -downloadPlatform iOS` |
| 起動済みのシミュレータ | `xcrun simctl boot "iPhone 17"`、またはSimulator.appを開く |
| テスト対象のアプリ | シミュレータにインストールしておく：`xcrun simctl install booted path/to/MyApp.app` |

アクションはシミュレータの起動もアプリのインストールも行いません。起動の失敗がテストの失敗と取り違えられないよう、[`shell`](/ja/reference/actions/shell)ステップなどの別のステップで行ってください。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-ios/releases)のノートの先頭に、コピーして使うアクションとコミットがあります。

```yaml
name: Settings
actions:
  ios: github.com/mozership/probe-ios@aed5436143e9536754e2123b953831f60cd635eb # v0.2.0
jobs:
  - name: settings
    steps:
      - name: Open General
        uses: ios
        timeout: 15m
        with:
          app: com.apple.Preferences
          actions:
            - name: launch
              args: ["-AppleLanguages", "(en)", "-AppleLocale", "en_US"]
            - name: tap
              selector: com.apple.settings.general
            - name: wait
              selector: type=navigationbar identifier=General
            - name: text
              id: back
              selector: type=button identifier=BackButton
        test: res.code == 0 && res.results.back == "Settings"
```

起動引数で、シミュレータの言語設定にかかわらずアプリを英語で起動しています。セレクタで指定するラベルが、言語設定によって変わらないようにするためです。

## 初回の実行

アクションを使う最初のステップは、ランナーをビルドします。ビルドには数十秒から数分かかり、そのことを`-v`なしでも表示します。

```
[WARN] ios: building the iOS test runner; the first run takes a few minutes, so give the step timeout: 10m if it runs out
```

ステップの既定のタイムアウトは5分で、初回のビルドでは足りないことがあります。そのため例では、最初のステップに専用の[`timeout`](/ja/reference/yaml-configuration#ステップ)を指定しています。以後のステップは、キャッシュ（`~/Library/Caches/probe-ios`、`PROBE_IOS_CACHE_DIR`を設定した場合はそのディレクトリ）からランナーを取り出します。アクションかXcodeが変わると、ランナーはビルドし直されます。同時に始まった複数のステップがビルドするのは1回だけで、ほかのステップはそのビルドを待ちます。

1台のシミュレータで同時に動かせるランナーは1つだけです。Probeの実行を同時に2つ走らせるなら、シミュレータを別々にしてください。同じ実行の中で同じシミュレータを使うステップは、順番に動きます。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|----------|---------|-------------|
| `app` | 文字列 | はい | - | 操作するアプリのバンドルID |
| `actions` | リスト | はい | - | 順に行う操作。[アクション](#アクション)を参照 |
| `device` | 文字列 | いいえ | 起動済みの唯一のシミュレータ | シミュレータの名前またはUDID。起動済みである必要があり、複数起動しているときは指定が必要 |
| `timeout` | 期間の文字列または秒数 | いいえ | `30s` | 最初から最後までの操作の制限時間。要素を待つ途中でこの時間が過ぎた操作は、失敗の理由を受け取るため、ランナーの応答をさらに最大1分待ちます。XCUITestは要素を探す前にアプリが落ち着くのを待ち、起動したばかりのシミュレータではそれに1分ほどかかることがあるためです。ランナーのビルドと起動は含まず、それらはステップ自身の`timeout`で制限します |

## アクション

`actions`の各要素は、`name`で操作を指定します。

| 操作 | フィールド | 内容 |
|--------|--------|--------------|
| `launch` | `args` | アプリを新しく起動します。動いていれば先に終了させ、`args`を起動引数として渡します |
| `activate` | | アプリを前面に出します。動いていなければ起動します |
| `terminate` | | アプリを終了します |
| `tap` | `selector` | 要素をタップします |
| `type` | `selector`、`value` | 要素をタップし、`value`を入力します |
| `wait` | `selector` | 要素が現れるまで待ちます |
| `text` | `selector`、`id` | 要素のラベルを読み、ラベルがなければ値を読んで、`res.results`に入れます |
| `tree` | `id` | XCUITestが表すアプリのアクセシビリティツリーを`res.results`に入れます。セレクタで指定できる内容がわかります |
| `screenshot` | `id` | 画面をPNGで保存し、そのパスを`res.filepaths`に入れます |

`id`は結果のキーで、省略すると操作の名前がキーになります。要素に対する操作は、`timeout`の残り時間だけ要素が現れるのを待ち、現れなければステップは失敗します。操作が受け付けないキー（綴りを間違えた`selector`など）を持つ要素は、ステップの実行前にエラーになります。

### セレクタ

セレクタは、空白で区切った`key=value`の組で要素を指定します。空白を含む値は二重引用符で囲みます。

| キー | 一致する対象 |
|-----|---------|
| `identifier`（または`id`） | アクセシビリティ識別子。SwiftUIの`.accessibilityIdentifier("login")`で設定する値 |
| `label` | アクセシビリティラベル。多くの場合、表示されているテキスト |
| `type` | 要素の種類：`alert`、`any`、`button`、`cell`、`image`、`link`、`navigationbar`、`other`、`searchfield`、`securetextfield`、`slider`、`switch`、`tabbar`、`text`、`textfield`、`textview` |
| `index` | 一致した要素のうち何番目か（0始まり）。省略すると最初の要素 |

`=`を含まないセレクタは識別子として扱うので、`login`は`identifier=login`と同じです。

## レスポンスオブジェクト

| フィールド | 説明 |
|-------|-------------|
| `res.code` | すべての操作が成功したときは`0` |
| `res.results` | `text`と`tree`が読んだ内容（キーごと） |
| `res.filepaths` | `screenshot`が保存したファイル（キーごと） |
| `res.device` | 操作したシミュレータのUDID |

最後まで終えられなかった操作は、ステップをアクションエラーで失敗させます。このとき実行は終了ステータス`3`で終わり、テストする`res`はありません。

```
actions[0] tap: no element matches label=No Such Row within 4.5s
```

## CI

GitHub ActionsではmacOSのランナーでワークフローを実行し、Probeを動かす前にシミュレータを作って起動します。[probe-action](https://github.com/mozership/probe-action)がProbeをインストールできるのはLinuxだけなので、このジョブでは`go install`でインストールします。ランナーを毎回ビルドしないよう、キャッシュのディレクトリを実行のあいだで保存しておきます。

```yaml
name: E2E
on: [pull_request]
jobs:
  e2e:
    runs-on: macos-26
    env:
      PROBE_IOS_CACHE_DIR: ${{ github.workspace }}/.probe-ios
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version: stable
      - run: go install github.com/mozership/probe/cmd/probe@latest
      - uses: actions/cache@v5
        with:
          path: .probe-ios/runner
          key: probe-ios-runner-${{ runner.os }}
      - name: Boot a simulator
        run: |
          UDID=$(xcrun simctl create e2e "iPhone 17" com.apple.CoreSimulator.SimRuntime.iOS-26-5)
          xcrun simctl bootstatus "$UDID" -b
      - run: probe e2e.yml
```

アクションはキャッシュをXcodeとビルドするランナーごとに分けて置くので、古くなったキャッシュは使われず、ビルドし直されます。
