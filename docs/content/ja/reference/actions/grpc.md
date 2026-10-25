# gRPCアクション

`grpc`アクションはgRPCのメソッドを呼び出します。サービス定義はサーバーリフレクションで解決するため、実行時に`.proto`ファイルは必要ありません。本番環境に多い、リフレクションのないサーバーは、[`proto`](#protoファイルによる検証)に指定した`.proto`ファイルの定義を使って呼び出します。`protocol: connect`を指定すると、HTTPの[Connectプロトコル](#connectプロトコル)で呼び出します。

## 基本的な構文

gRPCのステップでは、サーバー、サービス、メソッド、リクエストの内容を指定します。

```yaml
steps:
  - name: Get a user
    uses: grpc
    with:
      addr: "grpc.example.com:443"
      service: "user.v1.UserService"
      method: "GetUser"
      tls: true
      body: |
        {"id": "123"}
    test: res.status_code == "OK"
```

## パラメータ

以下のフィールドで呼び出し内容を指定します。いずれもテンプレート式を書けます。

| パラメータ | 型 | 必須 | デフォルト | 説明 |
|---|---|---|---|---|
| `protocol` | String | 任意 | `grpc` | `grpc`、または[Connectプロトコル](#connectプロトコル)で呼び出す`connect` |
| `addr` | String | 必須 | - | gRPCサーバーのホストとポート。`protocol: connect`ではサービスを提供しているURL |
| `service` | String | 必須 | - | 完全修飾のサービス名 |
| `method` | String | 必須 | - | メソッド名 |
| `body` | String、Object、List | 任意 | `""` | リクエストメッセージ（JSON）。オブジェクトはJSONにシリアライズされます。リストはリクエストのストリームのメッセージです。[ストリーミングのメソッド](#ストリーミングのメソッド)を参照 |
| `metadata` | Object | 任意 | `{}` | リクエストメタデータ（HTTPのヘッダーに相当） |
| `timeout` | String | 任意 | `30s` | 接続とリフレクションによる解決を含めた、呼び出しの制限時間。`"10s"`のようなGoのduration形式。それ以外の値はエラー |
| `tls` | Boolean | 任意 | `false` | TLSを使う |
| `insecure` | Boolean | 任意 | `false` | サーバー証明書の検証をスキップする。このとき`ca_file`は効かないが、クライアント証明書は送る |
| `cert_file` | String | 任意 | - | mTLS用のクライアント証明書。`key_file`と合わせて指定する |
| `key_file` | String | 任意 | - | mTLS用のクライアント鍵。`cert_file`と合わせて指定する |
| `ca_file` | String | 任意 | - | サーバー検証に使うCA証明書 |
| `codec` | String | 任意 | `json` | Connectの呼び出しのエンコード。`json`か`proto`。`protocol: connect`のときだけ |
| `max_messages` | Integer | 任意 | - | レスポンスのストリームがこの件数届いた時点で打ち切る。[ストリーミングのメソッド](#ストリーミングのメソッド)を参照 |
| `proto` | Objectまたは`false` | 任意 | - | `files`に挙げた`.proto`ファイルと呼び出しを照合し、違反があればステップを失敗させます。`false`はジョブの`defaults`が求める照合を外します。[.protoファイルによる検証](#protoファイルによる検証)を参照 |

## レスポンスオブジェクト

呼び出しの後、`res`に応答とgRPCのステータスが入ります。

| プロパティ | 型 | 説明 |
|---|---|---|
| `res.body` | Any | レスポンスメッセージ。JSON形式から解析したオブジェクトで、フィールド名は`createdAt`のようなlowerCamelCase |
| `res.rawbody` | String | 解析前のJSON形式のレスポンスメッセージ |
| `res.status_code` | String | gRPCのステータスコード（`OK`、`NOT_FOUND`など） |
| `res.status_message` | String | ステータスメッセージ |
| `res.metadata` | Object | サーバーが送ったヘッダーとトレーラー。各キーの最初の値が入り、同じ名前ならトレーラーが優先 |
| `rt.duration` | String | ラウンドトリップ時間（例: `"1.2ms"`） |
| `rt.sec` | Float | ラウンドトリップ時間（秒） |
| `status` | Integer | ステータスが`OK`のとき`0`、それ以外は`1` |
| `req` | Object | 送信したリクエスト |
| `res.violations` | Array | 呼び出しのうち`.proto`ファイルが許可しないもの。すべて許可されていれば空です。`proto`を指定したときだけ入ります |
| `res.messages` | Array | レスポンスをストリーミングするメソッドの全レスポンス。それぞれ`res.body`と同じくオブジェクトです。そのようなメソッドの場合だけ入ります |
| `res.complete` | Boolean | レスポンスのストリームを、呼び出しが打ち切ったのではなくサーバーが終えたか。レスポンスをストリーミングするメソッドの場合だけ入ります |
| `res.contract` | Object | `.proto`ファイルで呼び出しを対応付けた先。`spec`（サービスを宣言するファイル。`proto.files`に書いたパス）と、`users.v1.UserService/GetUser`のような`operation`が入ります。ファイルがそのメソッドを宣言している場合だけ入ります。[`probe coverage`](/ja/reference/cli-reference#coverage)はこれを数えます |

`res.status_code`は、呼び出しが終わったときのステータスの正式名です。`OK`、`CANCELLED`、`UNKNOWN`、`INVALID_ARGUMENT`、`DEADLINE_EXCEEDED`、`NOT_FOUND`、`ALREADY_EXISTS`、`PERMISSION_DENIED`、`RESOURCE_EXHAUSTED`、`FAILED_PRECONDITION`、`ABORTED`、`OUT_OF_RANGE`、`UNIMPLEMENTED`、`INTERNAL`、`UNAVAILABLE`、`DATA_LOSS`、`UNAUTHENTICATED`のいずれかになります。`OK`以外のステータスもサーバーの応答なので、ステップはそのままテストに進み、テストでそのステータスを期待できます。サーバーからステータスが得られなかった呼び出しだけが、エラーとしてステップを終わらせます。接続できないサーバーや、`.proto`ファイルが宣言していないのにリフレクションにもサービスが載っていない場合と、サーバーが応答する前に`timeout`を過ぎたり接続が切れたりした場合です。

## 使用例

以下では、メタデータを付けて呼び出す場合と、エラーが返ることを検証する場合の書き方を示します。

### メタデータを付けて呼び出す

認証情報などはメタデータとして渡します。

```yaml
steps:
  - name: Authenticated call
    id: get-user
    uses: grpc
    with:
      addr: "{{vars.grpc_addr}}"
      service: "user.v1.UserService"
      method: "GetUser"
      tls: true
      timeout: "10s"
      metadata:
        authorization: "Bearer {{vars.token}}"
      body: |
        {"id": "{{vars.user_id}}"}
    test: res.status_code == "OK"
    outputs:
      user: res.body
```

### エラーを検証する

ここではエラーが期待される結果です。そのためステータスコードを検証します。

```yaml
steps:
  - name: Unknown user returns NOT_FOUND
    uses: grpc
    with:
      addr: "{{vars.grpc_addr}}"
      service: "user.v1.UserService"
      method: "GetUser"
      tls: true
      body: |
        {"id": "does-not-exist"}
    test: res.status_code == "NOT_FOUND"
```

## .protoファイルによる検証

呼び出しは、サーバーがリフレクションで示す自身の定義を使って行います。そのため、サーバーが本来守るべき定義を守っているかどうかは分かりません。`proto`を指定すると、サービスを生成したリポジトリにある`.proto`ファイルなどとも呼び出しを照合します。リフレクションのないサーバーや、定義にそのメソッドがないサーバーは、ファイルから生成したクライアントと同じく、ファイルの定義を使って呼び出します。

```yaml
- name: Users
  defaults:
    grpc:
      addr: "{{vars.grpc_addr}}"
      service: user.v1.UserService
      proto:
        files: [./proto/user/v1/user.proto]
        import_paths: [./proto]
  steps:
    - name: Get a user
      uses: grpc
      with:
        method: GetUser
        body:
          id: "123"
      test: res.status_code == "OK" && res.body.user.name != ""
```

`files`は、`protoc`と同じく`import_paths`を使ってコンパイルします。`import_paths`を指定しなければ、Probeを実行したディレクトリを使います。各ファイルは、それを含む最初のインポートパスからのパスで名前が付き、インポートするときもその名前で指定します。ファイルは呼び出しの前にコンパイルするため、コンパイルできないファイルを指定したステップは、何も呼び出さずにアクションのエラーとして失敗します。

リクエストのボディ（JSON）がファイルのリクエストメッセージとして解釈できない場合、ステップは種類`contract_request`で失敗します。次の場合は種類`contract_response`で失敗します。

- サービスにそのメソッドをファイルが宣言していない
- サーバーのリクエストまたはレスポンスのメッセージ（入れ子のメッセージも含む）が、ファイルと名前が違う、ファイルが宣言するフィールドを持たない、または同じ番号のフィールドを別の名前、型、単数か複数かで宣言している
- レスポンスのフィールドが、ファイルがその番号に宣言するとおりにエンコードされていない（ファイルは`int32`と宣言しているのに文字列が届いた場合など）

`strict: true`を指定すると、ファイルが宣言していないフィールドをサーバーが宣言したり送ったりした場合も失敗します。指定しなければ、protobufが新しい版の定義で加わった未知のフィールドを受け入れるのと同じく、そのフィールドを通します。ファイルの定義で呼び出した場合は、サーバーの定義なしで行える範囲で照合します。リクエスト、レスポンスの読み直し、後述の制約は照合しますが、定義の突き合わせは行いません。リフレクションが示すサービスにそのメソッドがなければ、`contract_response`で失敗し、サーバーはその呼び出しに`UNIMPLEMENTED`で応答します。

サーバー自身の定義でも解釈できないリクエストのボディは送れないため、何も送らず、アクションのエラーではなく`contract_request`としてステップを失敗させます。このとき`res.status_code`は空です。`request: false`はサーバーとそのレスポンスだけを照合します。ファイルが許可しないものをわざと送るステップに使います。`OK`以外のステータスの応答には照合するメッセージがありません。違反はそれぞれ`res.violations`、端末、レポートに、`$.user.email`のようなフィールドとともに入ります。

### ファイルの注釈で指定した制約

proto3には必須のフィールドも値の範囲もないため、ファイルのフィールドに注釈がなければ、ファイルが示すのはメッセージの形だけです。ファイルが次の2種類の注釈を使っていれば、それも照合します。

- [protovalidate](https://protovalidate.com)のルール（`[(buf.validate.field)...]`と、メッセージやoneofのルール）は、リクエストとレスポンスの両方で照合します。ルールに違反したフィールドがあれば、`string.email`のようなルールとフィールドを示してステップを失敗させます。
- `google.api.field_behavior`は[AIP-203](https://google.aip.dev/203)のとおりに照合します。リクエストが`REQUIRED`のフィールドを持たなければ（リクエストが持つ入れ子のメッセージも含む）、`contract_request`で失敗します。レスポンスが`INPUT_ONLY`のフィールドを持っていれば、レスポンスが決して含めてはならないフィールドとして、`contract_response`で失敗します。

```protobuf
syntax = "proto3";
import "buf/validate/validate.proto";
import "google/api/field_behavior.proto";

message CreateUserRequest {
  string email = 1 [(buf.validate.field).string.email = true, (google.api.field_behavior) = REQUIRED];
  string password = 2 [(google.api.field_behavior) = INPUT_ONLY];
}
```

`buf/validate/validate.proto`と`google/api/field_behavior.proto`はProbeに組み込まれているため、インポートパスに置かなくてもファイルからインポートできます。インポートパスにあるコピーは読みません。

## Connectプロトコル

`protocol: connect`を指定すると、gRPCではなく[Connectプロトコル](https://connectrpc.com/docs/protocol)でメソッドを呼び出します。ConnectプロトコルはHTTPのPOSTなので、HTTP/1.1のプロキシやロードバランサーも通ります。ブラウザの[connect-web](https://connectrpc.com/docs/web/getting-started)クライアントが送る呼び出しと同じ経路です。connect-goやconnect-esで作ったサーバーが応答します。

```yaml
- name: Get a user
  uses: grpc
  with:
    protocol: connect
    addr: https://api.example.com/rpc
    service: users.v1.UserService
    method: GetUser
    body:
      id: "123"
    metadata:
      authorization: "Bearer {{vars.token}}"
  test: res.status_code == "OK" && res.body.user.name != ""
```

`addr`にはサービスを提供しているURLを、マウントしているパスがあればそれも含めて書きます。スキームのないホストとポートは`http`として扱い、`tls: true`なら`https`にします。呼び出し先は`<addr>/<service>/<method>`です。`metadata`はヘッダーとして送り、`timeout`は`Connect-Timeout-Ms`でサーバーにも伝えます。TLSのパラメータは、`https`のURLに対してgRPCの呼び出しと同じように効きます。

`codec`はメッセージのエンコードで、`json`（デフォルト）か`proto`です。HTTP/1.1の接続では運べないストリームが要るため、リフレクションは使いません。

- `codec: json`で`proto`がない場合、`body`は書いたとおりに送り、`res.body`はサーバーが返したJSONです。このとき`service`は`users.v1.UserService`のような完全な名前で書き、本文のフィールド名はサーバーが読む名前（connect-goなら`userId`のようなlowerCamelCase）にします。
- `proto`がある場合、リクエストはファイルの定義から組み立てます。そのため短いサービス名やsnake_caseのフィールド名もgRPCの呼び出しと同じように使え、`res.body`もgRPCの呼び出しと同じ形になります。ファイルとの照合は上で説明したとおりですが、比べるサーバーの定義はありません。`codec: json`では、レスポンスにファイルの宣言と型の違う値があると`contract_response`でステップが失敗し、`strict`のもとではファイルにないフィールドも同じく失敗します。
- `codec: proto`には`proto`が必要です。リクエストのエンコードとレスポンスの読み取りにその定義を使います。

`res`のフィールドはgRPCの呼び出しと同じです。HTTPステータス404で返る`{"code": "not_found", "message": "..."}`のようなConnectのエラーは、`res.status_code`が`NOT_FOUND`になり、メッセージも入ります。ConnectのコードはgRPCのコードと同じだからです。Connectのエラーを含まない応答もサーバーの応答として扱い、コードはconnect-goと同じくプロトコルの決まりに従って決めます。HTTPステータスから決める場合（プロキシの502のページなら`UNAVAILABLE`）はステータス行をメッセージにし、200でもcodecと違うContent-Typeなら`UNKNOWN`か`INTERNAL`にします。`res.metadata`はレスポンスヘッダーです。サーバーがメッセージの後に`Trailer-`を付けて送ったものは、それを外した名前で入り、同じ名前のヘッダーより優先されます。アクションのエラーになるのは、到達できないサーバーへの呼び出しや`timeout`を過ぎた呼び出しのように、応答が得られなかったときだけです。リダイレクトはたどりません。

ストリーミングのメソッドも、`proto`が宣言していれば`protocol: connect`で呼び出せます。HTTP/1.1でも通ります。詳しくは[ストリーミングのメソッド](#ストリーミングのメソッド)を参照してください。双方向にストリーミングするメソッドは呼び出しません。

## ストリーミングのメソッド

レスポンスかリクエストをストリーミングするメソッドも、ストリーミングしないメソッドと同じパラメータで、gRPCでも`protocol: connect`でも呼び出せます。呼び出しではすべてのリクエストを送ってから送信側のストリームを閉じ、それからレスポンスを読みます。

```yaml
- name: Watch users
  uses: grpc
  with:
    method: WatchUsers
    body:
      filter: active
    max_messages: 3
  test: res.status_code == "" && len(res.messages) == 3 && !res.complete

- name: Import users
  uses: grpc
  with:
    method: ImportUsers
    body:
      - name: Ada
      - name: Grace
  test: res.status_code == "OK" && res.body.imported == 2
```

- リクエストをストリーミングするメソッドには、`body`にリストを書きます。各要素がストリームの1メッセージで、順に送ります。オブジェクト1つなら唯一のメッセージになり、`[]`なら何も送りません。それ以外のメソッドにリストを書くとエラーです。
- レスポンスをストリーミングするメソッドでは、届いた順にすべてのメッセージが`res.messages`に入り、最後のメッセージが`res.body`に入ります。サーバーがストリームを終えた場合は`res.complete`が`true`になり、終えたときのステータスが`res.status_code`に入ります。
- `max_messages`を指定すると、その件数のレスポンスが届いた時点で呼び出しを打ち切ります。サーバーが終えないストリームに使います。このときサーバーはまだステータスを返していないため、`res.status_code`は空、`res.complete`は`false`です。レスポンスをストリーミングしないメソッドに指定するとエラーです。
- `timeout`で切れた場合のように、いくつかのレスポンスが届いた後でストリームが途切れたときは、届いたレスポンスと、途切れたときのステータス（`DEADLINE_EXCEEDED`など）が入り、`res.complete`は`false`です。レスポンスが1つも届いていなければ、応答のない呼び出しと同じくアクションのエラーです。`timeout`はサーバーにも伝えるため、サーバーが期限切れでストリームを自ら終えることもあり、その場合は`DEADLINE_EXCEEDED`で`res.complete`は`true`です。
- 双方向にストリーミングするメソッドは呼び出しません。何も送らずに、アクションのエラーでステップを失敗させます。

ストリーミングするかどうかは、呼び出しに使う定義（サーバーのリフレクションか、`proto`の`.proto`ファイル）で判断します。Connectの呼び出しにはリフレクションがないため、ストリーミングのメソッドは`proto`があるときだけ呼び出せます。`proto`がなければ、リストの`body`と`max_messages`はエラーになり、メソッドはストリーミングしないものとして呼び出すため、サーバーが拒否します。`proto`があれば、ストリームの各メッセージをファイルと照合し、違反は`$[1].user.email`のように何件目のメッセージかを付けて示します。サーバーの定義がファイルの宣言と違う形でストリーミングする場合は、`contract_response`でステップが失敗します。

## ガードの下での実行

grpcアクションは実行の[ガード](/ja/reference/cli-reference#--read-only)に従います。ガードが許可しない呼び出しは何も送らずに拒否し、種類`refused`でステップを失敗させます。

- `--allow-host`では、`addr`のホストとポートを確かめます。ポートがなければ、grpc-goと同じく443として扱います。前に付いた`dns:///`と`passthrough:///`は外して読みます。`dns://server/`のDNSサーバーも確かめ、ポートがなければ53として扱います。`unix:///tmp/grpc.sock`のようにホストのない宛先は拒否します。`protocol: connect`ではURLのホストを確かめ、ポートがなければスキームの既定のポートとして扱います。
- `--read-only`では、手元にあるメソッドの定義がすべて`option idempotency_level = NO_SIDE_EFFECTS;`で副作用がないと宣言しているときだけ呼び出します。手元の定義とは、リフレクションで得たサーバーの定義と、`proto`があればその`.proto`ファイルの定義です。リフレクションでの問い合わせは読み取りなので行います。Connectの呼び出しにはリフレクションがないため`proto`が必要で、ない場合は拒否します。

```protobuf
service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse) {
    option idempotency_level = NO_SIDE_EFFECTS;
  }
}
```

## 関連項目

- **[変数](/ja/reference/actions/variables)** - ステップで使える変数
- **[YAML設定](/ja/reference/yaml-configuration)** - ステップのプロパティ
