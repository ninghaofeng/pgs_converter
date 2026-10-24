package desktop

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

const (
	english = "en"
	chinese = "zh-CN"
)

type message struct {
	key  string
	args []any
}

func msg(key string, args ...any) message { return message{key: key, args: args} }
func (m message) String() string          { return fmt.Sprintf(m.key, m.args...) }

type localizedError struct{ message }

func (e localizedError) Error() string { return e.String() }
func (e localizedError) Unwrap() error {
	for _, arg := range e.args {
		if err, ok := arg.(error); ok {
			return err
		}
	}
	return nil
}

func uiError(key string, args ...any) error { return localizedError{msg(key, args...)} }

func initialLanguage(saved, system string) (string, error) {
	if saved == english || saved == chinese {
		return saved, nil
	}
	code := english
	if strings.HasPrefix(strings.ToLower(system), "zh") {
		code = chinese
	}
	if saved != "" {
		return code, uiError("Unsupported saved UI language %q; using the system language.", saved)
	}
	return code, nil
}

func translate(code, key string, args ...any) string {
	if code == chinese {
		value, ok := translations[key]
		if !ok {
			panic("missing desktop translation: " + key)
		}
		key = value
	}
	return fmt.Sprintf(key, args...)
}

func (w *window) tr(key string, args ...any) string {
	return translate(w.language, key, args...)
}

func (w *window) say(label *widget.Label, key string, args ...any) {
	if w.messages == nil {
		w.messages = make(map[*widget.Label]message)
	}
	w.messages[label] = msg(key, args...)
	label.SetText(w.tr(key, args...))
}

func (w *window) label(key string) *widget.Label {
	label := widget.NewLabel("")
	w.say(label, key)
	return label
}

func (w *window) button(key string, tapped func()) *widget.Button {
	button := widget.NewButton(w.tr(key), tapped)
	w.translators = append(w.translators, func() { button.SetText(w.tr(key)) })
	return button
}

func (w *window) check(key string) *widget.Check {
	check := widget.NewCheck(w.tr(key), nil)
	w.translators = append(w.translators, func() {
		check.Text = w.tr(key)
		check.Refresh()
	})
	return check
}

func (w *window) formItem(key string, field fyne.CanvasObject) *widget.FormItem {
	item := widget.NewFormItem(w.tr(key), field)
	w.translators = append(w.translators, func() { item.Text = w.tr(key) })
	return item
}

func (w *window) setLanguage(code string) {
	if code != english && code != chinese {
		w.showError(uiError("Unsupported UI language: %s", code))
		return
	}
	w.language = code
	w.app.Preferences().SetString("desktop-language", code)
	for _, update := range w.translators {
		update()
	}
	for label, message := range w.messages {
		label.SetText(w.tr(message.key, message.args...))
	}
	w.list.Refresh()
	w.win.Content().Refresh()
}

var translations = map[string]string{
	"Language":                               "界面语言",
	"Choose...":                              "请选择…",
	"Add file":                               "添加文件",
	"Add folder":                             "添加文件夹",
	"Remove selected":                        "移除选中项",
	"Clear queue":                            "清空队列",
	"Strict rendering diagnostics":           "严格检查渲染诊断",
	"Replace existing outputs after success": "成功转换后替换已有输出",
	"Include subfolders when adding folders": "添加文件夹时包含子文件夹",
	"Resolution":                             "分辨率",
	"Exact FPS":                              "精确帧率",
	"Color profile":                          "色彩模式",
	"HDR white (nits)":                       "HDR 参考白（尼特）",
	"Input encoding":                         "输入编码",
	"SRT font family":                        "SRT 字体名称",
	"SRT font size":                          "SRT 字号",
	"SRT margin":                             "SRT 边距",
	"Font folders (one per line)":            "字体目录（每行一个）",
	"Concurrency (1..32)":                    "并发数（1～32）",
	"Auto: canvas height / 24":               "自动：画布高度 ÷ 24",
	"Add font folder":                        "添加字体目录",
	"SRT style settings do not override ASS/SSA styles.": "SRT 样式设置不会覆盖 ASS／SSA 原有样式。",
	"Render at time":      "按时间预览",
	"First subtitle":      "首句字幕",
	"Previous":            "上一句",
	"Next":                "下一句",
	"Time (ms)":           "时间（毫秒）",
	"Copy log":            "复制日志",
	"Subtitle preview":    "字幕预览",
	"Log / error details": "日志／错误详情",
	"Start conversion":    "开始转换",
	"Cancel remaining":    "取消剩余任务",
	"Output folder":       "输出目录",
	"Browse":              "浏览",
	"Flat output folder; duplicate basenames are rejected. 4K: start with concurrency 1 or 2.": "输出平铺到指定目录，同名文件会被阻止。4K 建议先使用 1～2 个并发任务。",
	"Drop SRT / ASS / SSA files or folders into this window.":                                  "将 SRT／ASS／SSA 文件或文件夹拖放到此窗口。",
	"Ready. Settings are snapshotted when conversion starts.":                                  "就绪。转换开始时会固定本次任务的设置。",
	"Style preview only: SDR display; no SUP quantization or HDR luminance simulation.":        "仅为 SDR 样式预览，不显示 SUP 量化或模拟 HDR 实际亮度。",
	"Use First subtitle or enter an exact time. Preview is SDR style only.":                    "点击“首句字幕”或输入精确时间。此处仅为 SDR 样式预览。",
	"Select a file for SDR style preview; no SUP quantization or HDR luminance simulation.":    "选中文件以预览 SDR 样式；不显示 SUP 量化或模拟 HDR 实际亮度。",
	"Select a file to preview or remove it.":                                                   "选中文件进行预览或将其移除。",
	"%s\nOutput: %s":                                                                           "%s\n输出：%s",
	"Discovering subtitle files...":                                                            "正在查找字幕文件…",
	"%d queued; inspect settings and output folder before starting.":                           "队列中有 %d 个文件；开始前请检查设置和输出目录。",
	"Converting with snapshotted settings...":                                                  "正在使用本次固定设置转换…",
	"Cancelling; waiting for active native calls and output cleanup...":                        "正在取消；等待当前原生渲染调用结束并清理临时输出…",
	"Cancelling; waiting for output cleanup...":                                                "正在取消；等待临时输出清理完成…",
	"Waiting":                  "等待中",
	"Blocked: %s":              "已阻止：%s",
	"Converting":               "转换中",
	"%.1f%% (%d display sets)": "%.1f%%（%d 个显示集）",
	"Finalizing output...":     "正在保存最终输出…",
	"Failed: %s":               "失败：%s",
	"Cancelled":                "已取消",
	"Saved (%d display sets)":  "已保存（%d 个显示集）",
	"Not processed":            "未处理",
	"Cancelled before start":   "开始前已取消",
	"Finished: %d saved, %d failed, %d cancelled/not processed.":                        "已结束：%d 个已保存，%d 个失败，%d 个取消／未处理。",
	"Rendering SDR style preview...":                                                    "正在渲染 SDR 样式预览…",
	"Preview failed; see log.":                                                          "预览失败，请查看日志。",
	"%s at %d ms | SDR style preview; no SUP quantization or HDR luminance simulation.": "%s，%d 毫秒｜SDR 样式预览，不显示 SUP 量化或模拟 HDR 实际亮度。",
	"No matching subtitle boundary. Use First subtitle to load this file.":              "没有符合条件的字幕起点。请先点击“首句字幕”加载文件。",
	"Conversion in progress":                                                            "转换尚未结束",
	"Cancel remaining work and close after temporary outputs are cleaned up?":           "是否取消剩余任务，并在临时输出清理完成后关闭？",
	"Cancel and close":                   "取消任务并关闭",
	"Keep converting":                    "继续转换",
	"Error":                              "错误",
	"OK":                                 "确定",
	"Cancel":                             "取消",
	"Open":                               "打开",
	"Choose subtitle file":               "选择字幕文件",
	"Choose folder":                      "选择文件夹",
	"Select folder":                      "选择此文件夹",
	"Details (original diagnostic):\n%s": "详情（保留原始诊断信息）：\n%s",
	"only local files and folders are supported: %s":                       "仅支持本地文件和文件夹：%s",
	"saved settings are invalid; defaults restored: %v":                    "保存的设置无效，已恢复默认设置：%v",
	"Unsupported saved UI language %q; using the system language.":         "保存的界面语言 %q 不受支持，已使用系统语言。",
	"Unsupported UI language: %s":                                          "不支持的界面语言：%s",
	"queued input changed on disk; clear the queue and add it again":       "队列中的输入文件已在磁盘上发生变化，请清空队列后重新添加。",
	"wait for discovery or conversion to finish before changing the queue": "请等待文件查找或转换完成后再修改队列。",
	"no SRT, ASS or SSA files found":                                       "未找到 SRT、ASS 或 SSA 字幕文件。",
	"choose an output folder":                                              "请选择输出目录。",
	"add subtitle files first":                                             "请先添加字幕文件。",
	"%s: %v; remove blocked inputs before starting":                        "%s：%v；开始前请移除被阻止的输入文件。",
	"save settings: %v":                                                    "保存设置失败：%v",
	"select a subtitle file first":                                         "请先选中一个字幕文件。",
	"preview time must be integer milliseconds":                            "预览时间必须为整数毫秒。",
	"resolution must be WIDTHxHEIGHT":                                      "分辨率格式必须为“宽x高”，例如 1920x1080。",
	"invalid width: %v":                                                    "宽度无效：%v",
	"invalid height: %v":                                                   "高度无效：%v",
	"invalid HDR white: %v":                                                "HDR 参考白亮度无效：%v",
	"invalid SRT font size: %v":                                            "SRT 字号无效：%v",
	"invalid SRT margin: %v":                                               "SRT 边距无效：%v",
	"invalid SRT font, size or margin":                                     "SRT 字体名称、字号或边距无效。",
	"concurrency must be in 1..32":                                         "并发数必须为 1～32 之间的整数。",
	"Invalid frame rate; use a supported fraction such as 24000/1001.\n%s": "帧率无效，请使用支持的精确帧率，例如 24000/1001。\n%s",
	"Invalid color profile or HDR reference white.\n%s":                    "色彩模式或 HDR 参考白亮度无效。\n%s",
	"Invalid canvas, encoding or font folders.\n%s":                        "画布尺寸、输入编码或字体目录无效。\n%s",
}
