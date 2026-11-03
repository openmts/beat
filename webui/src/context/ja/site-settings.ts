// 日文目录：与其它语言并行维护，补齐后由 shippedLocales 统一发布。
export const jaSiteSettings: Record<string, string> = {
  "settings.site": "サイト設定",
  "settings.site_description":
    "公開される名称、ブランディング、訪問者向けの見せ方。",
  "settings.identity": "アイデンティティ",
  "settings.site_title": "サイト名",
  "settings.site_title_hint":
    "ブラウザーのタイトルとアプリのヘッダーに表示されます。",
  "settings.description": "サイトの説明",
  "settings.description_hint":
    "ブラウザーと検索プレビューのページ説明として使用されます。",
  "settings.branding": "ブランディング",
  "settings.logo_url": "ロゴ URL",
  "settings.logo_hint": "HTTPS の URL またはルート相対パスを使用してください。",
  "settings.favicon_url": "ファビコン URL",
  "settings.favicon_hint": "ブラウザーのタブとブックマークに表示されます。",
  "settings.appearance": "表示",
  "settings.default_theme": "既定のテーマ",
  "settings.theme_system": "システムに従う",
  "settings.theme_light": "ライト",
  "settings.theme_dark": "ダーク",
  "settings.default_theme_hint":
    "訪問者がローカルのテーマを選択するまで適用されます。",
  "settings.public_display": "公開表示",
  "settings.show_ip": "ノードの IP アドレスを表示",
  "settings.show_ip_hint":
    "無効にすると、公開 REST とライブ応答はノードのホストを伏せます。",
  "settings.address_mode": "公開するノードアドレス",
  "settings.address_hidden": "非表示",
  "settings.address_masked": "マスク",
  "settings.address_full": "完全",
  "settings.address_hint":
    "非表示はアドレスを削除し、マスクはネットワークプレフィックス (IPv4 の先頭オクテットまたは IPv6 の先頭 32 ビット) のみを保持し、完全は保存された値を公開します。",
  "settings.deployment": "デプロイアクセス",
  "settings.deployment_hint":
    "これらの値はサーバー環境から取得されます。ここでは編集できず、オリジン一覧がブラウザーに送信されることはありません。",
  "settings.deployment_unavailable": "利用不可",
  "settings.external_url": "外部 URL",
  "settings.external_url_unset": "未設定",
  "settings.api_origins": "公開 REST のオリジン",
  "settings.ws_origins": "公開 WebSocket のオリジン",
  "settings.origins_all": "すべての公開オリジン",
  "settings.origins_same": "同一オリジン",
  "settings.origins_configured": "{count} 件のオリジンを設定済み",
  "settings.address_privacy_note":
    "訪問者の身元と活動は記録されません。公開テレメトリは集計値のみで、解析サンプルが SQLite に書き込まれることはありません。",
  "settings.show_network": "ネットワーク品質を表示",
  "settings.show_network_hint":
    "公開ダッシュボードのネットワーク品質セクションを制御します。",
  "settings.show_accelerators": "GPU とアクセラレーターを表示",
  "settings.show_accelerators_hint":
    "グローバル上書きです。オフにすると、すべてのノードの指標ページで GPU とアクセラレーターのセクションが非表示になります。",
  "settings.accelerator_node_visibility": "このノードを指標ページに表示",
  "settings.accelerator_node_visibility_hint":
    "グローバルスイッチがオンの場合に、このノードの GPU とアクセラレーターのセクションを表示するか制御します。",
  "settings.accelerator_node_global_off":
    "グローバルスイッチはオフです。このノードの設定は保存され、グローバルスイッチを再びオンにすると有効になります。",
  "settings.public_telemetry": "匿名の集計テレメトリ",
  "settings.public_telemetry_hint":
    "固定された少数の粗いページイベントを数えます。身元、アドレス、URL、自由記述の詳細は一切収集されません。",
  "settings.telemetry_enabled": "匿名のインタラクション数を収集する",
  "settings.telemetry_ceiling": "セッションあたりのサンプル上限",
  "settings.telemetry_ceiling_hint":
    "ブラウザーセッションごとに適用され、サーバーも各送信元をレート制限します。",
  "settings.telemetry_retention": "集計の保持期間 (日)",
  "settings.telemetry_retention_hint":
    "集計カウンターはこの日数を過ぎると削除されます。",
  "settings.telemetry_proof_hint":
    "テレメトリポリシーを変更する前に、現在のパスワードと認証アプリのコードで本人確認を行ってください。",
  "settings.telemetry_password": "現在のパスワード",
  "settings.telemetry_code": "認証アプリのコード",
  "settings.telemetry_cancel": "キャンセル",
  "settings.telemetry_confirm_action": "確認",
  "settings.telemetry_save": "公開テレメトリポリシーを保存",
  "settings.telemetry_saved": "公開テレメトリポリシーを保存しました",
  "settings.telemetry_unavailable": "公開テレメトリポリシーは利用できません",
  "settings.proxy_title": "送信プロキシ",
  "settings.proxy_description":
    "サーバー側のダウンロードと外部連携を HTTP、HTTPS、SOCKS5、SOCKS5H プロキシ経由にします。保存後、新しいリクエストからすぐに適用されます。",
  "settings.proxy_enabled_badge": "有効",
  "settings.proxy_disabled_badge": "無効",
  "settings.proxy_enable": "送信プロキシを使用",
  "settings.proxy_enable_hint":
    "サーバーを再起動せず、新しいバックグラウンドリクエストに保存済み設定を使用します。",
  "settings.proxy_url": "プロキシ URL",
  "settings.proxy_url_hint":
    "スキームとポートを含めてください。例: http://127.0.0.1:7890、socks5h://proxy.example:1080",
  "settings.proxy_username": "ユーザー名",
  "settings.proxy_username_hint":
    "任意。認証情報はサーバーのアプリケーション秘密鍵で暗号化して保存されます。",
  "settings.proxy_password": "パスワード",
  "settings.proxy_password_saved":
    "パスワードは保存済みです。空欄のままにすると変更されません。",
  "settings.proxy_password_hint": "任意。認証不要の場合は空欄にしてください。",
  "settings.proxy_clear_password": "保存済みパスワードを消去",
  "settings.proxy_clear_password_hint":
    "ユーザー名は保持されます。認証が不要になった場合に使用します。",
  "settings.proxy_scope":
    "MMDB と MaxMind のダウンロード、GeoIP 照会、Agent リリース、テーママーケット、Webhook、OIDC に適用されます。SMTP と SSH は各ネイティブ転送を使用します。",
  "settings.proxy_revision": "設定リビジョン {revision}",
  "settings.proxy_save": "プロキシ設定を保存",
  "settings.proxy_saved": "送信プロキシ設定を保存しました",
  "settings.proxy_conflict":
    "別のセッションで設定が変更されました。最新値を再読み込みしました。確認して再度保存してください。",
  "settings.proxy_save_failed": "送信プロキシ設定を保存できませんでした",
  "settings.proxy_unavailable": "送信プロキシ設定を利用できません",
  "settings.proxy_confirm_title": "送信プロキシを保存",
  "settings.proxy_confirm_description":
    "所有者限定のネットワーク経路変更です。本人確認を行ってください。",
  "settings.proxy_test": "保存済みプロキシをテスト",
  "settings.proxy_test_hint":
    "保存済み設定をテストします。未保存の変更は先に保存してください。",
  "settings.proxy_test_title": "送信プロキシをテスト",
  "settings.proxy_test_description":
    "Beat は保存済みプロキシ経由で固定の example.com 公開ページへアクセスします。プロキシ認証情報は返しません。",
  "settings.proxy_test_success": "プロキシ接続に成功しました（{latency} ミリ秒）",
  "settings.proxy_test_failed": "送信プロキシをテストできませんでした",
  "settings.saved": "サイト設定を保存しました",
  "settings.preview": "ブランドのプレビュー",
};
