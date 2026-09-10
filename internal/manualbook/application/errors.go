package application

// ReportedError は「原因はもう表示してある」失敗。手順の途中で資料ごとの失敗を
// その場で出し終えているので、最後にもう一度エラー文を出さず、終了コードだけ
// 伝えたいときに返す。cli はこの型なら黙ってその値で終了する。
type ReportedError int

func (e ReportedError) Error() string { return "失敗した (原因は表示済み)" }

func (e ReportedError) ExitCode() int { return int(e) }
