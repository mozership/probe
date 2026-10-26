# Androidアクション

Androidアクションは、Androidのエミュレータまたは実機の上のアプリを操作します。アプリの起動、タップ、文字の入力、戻るボタン、要素の待機、テキストの読み取り、スクリーンショットの保存ができます。LinuxとmacOSで動きます。

[外部アクション](/ja/guide/concepts/actions#外部アクション)として[mozership/probe-android](https://github.com/mozership/probe-android)で公開しています。ワークフローが初めて使うときにProbeがダウンロードし、固定したコミットの`action.yml`が示すSHA-256と一致する実行ファイルだけを実行します。

## 仕組み

アクションはadbだけで動きます。ビルドするものはなく、端末に入れるのもテスト対象のアプリだけです。要素に対する各操作は、`uiautomator dump`で画面を読み、要素が現れるまで読み直してから、`input`で操作します。タップは要素の中央への`input tap`、入力は`input text`です。日本語など`input text`で入力できない文字は、[ADBKeyBoard](#文字の入力)で入力します。

| 必要なもの | 用意の仕方 |
|------|-----|
| adb | Android SDK Platform-Tools。PATHか`$ANDROID_HOME/platform-tools`に置く |
| 起動済みのエミュレータまたは実機 | `emulator -avd <name>`、またはUSBデバッグを有効にした実機。`adb devices`で`device`と表示されること |
| テスト対象のアプリ | 端末にインストールしておく：`adb install path/to/app.apk` |

アクションはエミュレータの起動もアプリのインストールも行いません。起動の失敗がテストの失敗と取り違えられないよう、[`shell`](/ja/reference/actions/shell)ステップなどの別のステップで行ってください。

エミュレータのアニメーションは、`adb shell settings put global animator_duration_scale 0`と、`window_`および`transition_`で始まる同様の設定でオフにしてください。uiautomatorは動いている途中の画面を読めないので、アクションはアニメーションが終わるまで待つことになります。

## 基本的な構文

ワークフローでは40文字のコミットSHAでアクションを固定します。各[リリース](https://github.com/mozership/probe-android/releases)のノートの先頭に、コピーして使うアクションとコミットがあります。

```yaml
name: Settings
actions:
  android: github.com/mozership/probe-android@bd855d4c4c0a62c662f7e059e4ff5b0586c63732 # v0.2.0
jobs:
  - name: settings
    steps:
      - name: Open Network & internet
        uses: android
        with:
          app: com.android.settings
          actions:
            - name: launch
            - name: tap
              selector: text="Network & internet"
            - name: text
              id: airplane
              selector: text="Airplane mode"
        test: res.code == 0 && res.results.airplane == "Airplane mode"
```

画面は端末に残るので、後のステップは前のステップが終えた画面から操作を続けます。

## パラメータ

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|-----------|------|----------|---------|-------------|
| `app` | 文字列 | はい | - | 操作するアプリのパッケージ名 |
| `actions` | リスト | はい | - | 順に行う操作。[アクション](#アクション)を参照 |
| `device` | 文字列 | いいえ | 唯一の端末 | 端末のシリアル（`emulator-5554`など）またはモデル名。adbに複数の端末が表示されるときは指定が必要 |
| `timeout` | 期間の文字列または秒数 | いいえ | `30s` | 最初から最後までの操作の制限時間 |

## アクション

`actions`の各要素は、`name`で操作を指定します。

| 操作 | フィールド | 内容 |
|--------|--------|--------------|
| `launch` | | ランチャーのアイコンと同じようにアプリを新しく起動します。動いていれば先に終了させます |
| `activate` | | アプリを元の状態のまま前面に出します。動いていなければ起動します |
| `terminate` | | アプリを終了します |
| `back` | | 戻るボタンを押します |
| `tap` | `selector` | 要素の中央をタップします |
| `type` | `selector`、`value` | 要素をタップし、`value`を入力します。[文字の入力](#文字の入力)を参照 |
| `wait` | `selector` | 要素が画面に現れるまで待ちます |
| `text` | `selector`、`id` | 要素のテキストを読み、テキストがなければコンテンツの説明を読んで、`res.results`に入れます |
| `tree` | `id` | 画面を1行に1要素の形で`res.results`に入れます。セレクタで指定できる内容がわかります |
| `screenshot` | `id` | 画面をPNGで保存し、そのパスを`res.filepaths`に入れます |

`id`は結果のキーで、省略すると操作の名前がキーになります。要素に対する操作は、`timeout`の残り時間だけ要素が現れるまで画面を読み直し、現れなければステップは失敗します。操作が受け付けないキー（iOSアクションの`args`など）を持つ要素は、ステップの実行前にエラーになります。

`tree`は、画面の要素を深さに応じて字下げし、1行に1つずつ並べます。

```
TextView id=android:id/title text="Network & internet" [200,540][700,600]
EditText id=com.android.settings:id/ssid text="Wi-Fi" [20,230][256,278] focused
```

### セレクタ

セレクタは、空白で区切った`key=value`の組で要素を指定します。空白を含む値は二重引用符で囲みます。

| キー | 一致する対象 |
|-----|---------|
| `id` | リソースID。`com.example:id/login`のように全体でも、`login`のように名前だけでも指定できます |
| `text` | 表示されているテキスト |
| `desc` | コンテンツの説明。アイコンなどでスクリーンリーダーが読み上げる文字列です |
| `class` | クラス。`android.widget.Button`のように全体でも、`Button`のように名前だけでも指定できます |
| `index` | 一致した要素のうち、`tree`の並び順で何番目か（0始まり）。省略すると最初の要素 |

`=`を含まないセレクタはリソースIDとして扱うので、`login`は`id=login`と同じです。

## 文字の入力

`type`は、表示可能なASCII文字を`input text`で入力します。`input text`はどの端末でも使えます。ただし日本語などそれ以外の文字や、`input text`が空白と解釈する`%s`は入力できません。そうした文字は、adbから送った文字列を入力するIMEである[ADBKeyBoard](https://github.com/senzhk/ADBKeyBoard)で入力します。

```yaml fragment
- name: type
  selector: id=ssid
  value: 日本語
```

ADBKeyBoardは、[リリース](https://github.com/senzhk/ADBKeyBoard/releases)の`keyboardservice-debug.apk`を`adb install`して、端末に入れておく必要があります。アクションはADBKeyBoardをインストールしません。入っていない端末では、ADBKeyBoardが必要なステップを最初の操作の前に失敗させます。インストール以外の設定は要りません。そうした文字を入力するステップは、最初の操作の前にADBKeyBoardをIMEに切り替え（有効でなければ有効にし）、ステップが終わると、失敗した場合も含めて元のIMEに戻します。ADBKeyBoardは準備が整う前に送られた文字列を捨てるので、アクションはフォーカスのある要素に文字列が入るまで画面を読み直し、入っていなければ送り直します。

uiautomatorで画面を読むと、ソフトキーボードが閉じます。フォーカスは要素に残るので`type`はそのまま入力できますが、キーボードが閉じたときに画面を変えるアプリは、そこから先を操作できません。たとえば設定アプリの検索は、キーボードが閉じると検索画面ごと閉じます。

## レスポンスオブジェクト

| フィールド | 説明 |
|-------|-------------|
| `res.code` | すべての操作が成功したときは`0` |
| `res.results` | `text`と`tree`が読んだ内容（キーごと） |
| `res.filepaths` | `screenshot`が保存したファイル（キーごと） |
| `res.device` | 操作した端末のシリアル |

最後まで終えられなかった操作は、ステップをアクションエラーで失敗させます。このとき実行は終了ステータス`3`で終わり、テストする`res`はありません。

```
actions[1] tap: no element matches text="No Such Row" within 5.0s
```

## CI

GitHub ActionsのUbuntuのランナーでは、KVMを使ってエミュレータを動かせます。[android-emulator-runner](https://github.com/ReactiveCircus/android-emulator-runner)がエミュレータを起動してアニメーションをオフにし、起動しているあいだにスクリプトを実行します。

```yaml
name: E2E
on: [pull_request]
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - name: Install Probe
        uses: mozership/probe-action@v1.3.0
        with:
          install-only: true
      - name: Enable KVM
        run: |
          echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules
          sudo udevadm control --reload-rules
          sudo udevadm trigger --name-match=kvm
      - uses: reactivecircus/android-emulator-runner@v2
        with:
          api-level: 35
          target: google_apis
          arch: x86_64
          script: probe e2e.yml
```
