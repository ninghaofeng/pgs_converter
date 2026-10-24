# pgs-converter

[English](./README.md) | 简体中文

将 SRT、ASS 和 SSA 文本字幕转换为 PGS（`.sup`）位图字幕。
命令行工具和 PGS 编码器使用 Go 编写，通过 libass 渲染字体、定位、图层、
矢量绘图、淡入淡出、移动、变换及卡拉 OK 效果。

当前为初版实现，**不是蓝光制盘或蓝光合规认证工具**。
本工具不编码视频、不进行 OCR，也不直接封装 MKV。

## 构建

### 推荐：一条命令编译并打包

在项目目录中执行以下命令即可。**无需再填写 `Binary`、`Prefix`、
`Destination`**：脚本会识别本机架构、查找依赖、运行测试和检查、
编译程序、收集动态库及许可证，并测试打包后的程序。

**Windows：在 PowerShell 或命令提示符中运行**

```powershell
.\scripts\build-windows.cmd
```

默认只检查依赖，缺少依赖时会给出提示，**不会自动安装软件**。
如果希望脚本帮助安装，可以显式允许：

```powershell
.\scripts\build-windows.cmd -InstallDependencies
```

此选项允许通过 winget 安装缺少的 Go/MSYS2，并通过 MSYS2 的 pacman
安装对应的编译器、pkgconf 和 libass。自动安装 Go/MSYS2 时，
系统需要已有微软的 **App Installer（应用安装程序）**，即能使用 `winget`。
安装器可能请求管理员授权。安装完成或 MSYS2 核心更新后，
如提示重新打开终端或重试，按提示操作后再次运行命令即可。

如果 MSYS2 已安装在自定义位置，可以指定其根目录：

```powershell
.\scripts\build-windows.cmd -Msys2Root D:\tools\msys64
```

这里填写 MSYS2 根目录，而不是里面的 `ucrt64` 或 `clangarm64` 子目录。
启动器仅对子 PowerShell 进程临时使用执行策略绕过，
不会永久更改系统的执行策略。

**macOS：在终端中运行**

```sh
bash scripts/build-macos.sh
```

允许安装缺少的依赖时使用：

```sh
bash scripts/build-macos.sh --install-dependencies
```

需要事先安装 [Homebrew](https://brew.sh)。显式安装选项允许安装或升级 Go，
以及安装 pkgconf/libass。如果缺少 Apple 命令行工具，脚本会打开系统安装器；
完成安装后重新运行即可。

两个入口都只构建**当前本机架构**，不通过修改 `GOARCH` 交叉编译；
Go、C 工具链及原生库的架构必须匹配。
FFmpeg 不是必需依赖：相应工具可用时会执行独立解码测试及打包输出探测，
不可用时会明确报告跳过。
即使没有开启依赖安装，构建也可能下载 Go 模块，打包还需要下载许可证文本；
默认模式不等于离线模式。

中间编译文件放在 `build/`，可运行的完整程序包放在 `dist/`。
每次运行都会生成唯一命名的包目录、Windows 的 `.zip` 或 macOS 的 `.tar.gz`
压缩包，以及对应的 `.sha256` 校验文件，不会删除之前成功生成的程序包。
使用包目录中的可执行文件时，请保留旁边的动态库。
查看脚本帮助可运行 `.\scripts\build-windows.cmd -?`
或 `bash scripts/build-macos.sh --help`。

### 手动构建（进阶）

需要 Go 1.24 或更高版本、支持 cgo 的 C 编译器、pkg-config 和 libass 开发文件。
已在 macOS ARM64 上使用 libass 0.17.5 完成本地开发验证。
转换程序运行时不依赖 FFmpeg；开发测试使用 FFmpeg 进行独立解码验证。

#### macOS

```sh
brew install libass pkgconf
go build -trimpath -o pgs-converter ./cmd/pgs-converter
./pgs-converter version
```

Go 和 Homebrew 依赖需要与目标架构一致。
仅设置 `GOARCH` 并不能完成原生 C 依赖的交叉编译。

#### Windows

x64 使用 Windows 原生 Go 和 MSYS2 的 **UCRT64** 工具链；
ARM64 使用 Windows 原生 Go 和 **CLANGARM64** 工具链。
安装对应环境的编译器、libass 和 pkgconf 软件包后：

- 将该环境的 `bin` 目录加入 `PATH`。
- 设置 `CGO_ENABLED=1`。
- UCRT64 设置 `CC=gcc`；CLANGARM64 设置 `CC=clang`。
- 将 `PKG_CONFIG_PATH` 指向该环境的 `lib/pkgconfig` 目录。

然后运行：

```powershell
go build -trimpath -o pgs-converter.exe ./cmd/pgs-converter
```

GitHub Actions 已配置使用上述入口执行 macOS ARM64/x64 和 Windows ARM64/x64
四个目标的构建、测试及共享原生库打包流程。**目前只有 macOS ARM64 完成本地运行验证；
其他目标及其打包流程需要通过实际 CI 和干净环境测试后才能确认。**

CI 使用软件包管理器提供的原生依赖，并记录已安装版本；
目前尚未实现基于锁定源码版本的可重现原生依赖构建。

## 单文件转换

必须显式指定输出分辨率和帧率。
ASS 的 `PlayRes` 是脚本坐标系统，不能作为可靠的视频分辨率声明。

```sh
pgs-converter convert movie.ass \
  --size 1920x1080 --fps 24000/1001 -o movie.sup

pgs-converter convert movie.srt \
  --size 1920x1080 --fps 24 \
  --font-dir ./fonts --font "Noto Sans CJK SC" \
  --font-size 45 --margin 45 -o movie.sup

pgs-converter convert legacy.ssa \
  --encoding gb18030 --size 1920x1080 --fps 25 -o legacy.sup
```

支持的帧率为：`24000/1001`、`24`、`25`、`30000/1001`、`30`、`50`、
`60000/1001`、`60`。不接受 `23.976` 等近似小数表示。

画布宽高必须能存入 PGS 的 16 位字段，总像素数不得超过 8,294,400，
即 3840 × 2160 的像素数量。工具接受某个尺寸，不代表硬件播放器支持该尺寸。

SRT 默认使用系统 `sans-serif` 字体族，字号和边距默认为画布高度除以 24，
最小为 1 像素。`--font`、`--font-size` 和 `--margin` 仅影响 SRT；
ASS/SSA 保留原脚本样式。

SRT 支持多行文本、重叠事件、HTML 实体、嵌套的 `b`/`i`/`u`、
`font color="#RRGGBB"` 和 `br` 标记。
未知、不配对或不支持的标记会明确报错，不会被静默移除。

转换为 libass 内存中的 ASS 脚本时，会保护原文本中的花括号和反斜杠转义序列，
避免被当作 ASS 指令执行。原始 SRT 毫秒时间会恢复到解析后的字幕事件上，
不会直接截断到 ASS 的百分之一秒精度。

## 批量转换

```sh
pgs-converter batch ./subtitles \
  --recursive --size 1920x1080 --fps 24000/1001 \
  --output-dir ./output --jobs 2

pgs-converter batch one.srt two.ass \
  --size 1920x1080 --fps 24 --output-dir ./output
```

- 默认不递归遍历目录；添加 `--recursive` 后才处理子目录。
- 保留相对目录结构。多个输入根目录分别使用相对于各自根目录的路径。
  多个输入映射到同一输出时，所有冲突项都会在转换前被拒绝，包括仅大小写不同的路径。
- 单个文件失败不会中断其他文件。进度、警告、输入输出映射和最终汇总均写入标准错误流。
- `--jobs` 默认为 1，支持 1 到 32。每个任务拥有独立的 libass 渲染器。
  提高并发数会增加原生字体缓存和图像内存的占用。
- 默认保护已有文件，只有指定 `--overwrite` 才允许替换。
  转换先写入目标目录中的临时文件，成功后再提交为最终文件。
  禁止覆盖时使用硬链接提交：文件系统不支持硬链接会明确失败，而不会冒险覆盖。
  允许覆盖时使用操作系统的重命名操作；并非所有 Windows 文件系统都保证原子替换。
- Ctrl+C 会停止调度，在渲染或编码操作之间取消活动任务，并清理临时文件。
  不会强制打断正在执行的单次高开销原生操作。

退出码：

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功、显示帮助或显示版本 |
| `1` | 一个或多个文件转换失败 |
| `2` | 命令参数或全局配置错误 |
| `130` | 任务被取消 |

## 字体与文本编码

默认启用系统字体。可以重复指定 `--font-dir`，从多个目录加载额外的 TTF、
OTF 或 TTC 字体文件，不递归扫描子目录。
工具不捆绑字体；若需要不同系统输出一致，请提供相同且具有适当授权的字体文件。

工具会报告 libass 警告、不可用字形诊断以及能够检测到的字体回退失败。
`--strict` 会将这些诊断以及采样后完全不可见的短事件视为文件转换失败。
正常的系统字体选择可能在不发出警告的情况下替换字体族，
因此严格模式**不保证字体完全一致，也不等于完整的字形覆盖预检**。

`--encoding auto` 支持 UTF-8（可带或不带 BOM）以及带 BOM 的 UTF-16。
显式编码选项包括：`utf-8`、`utf-16le`、`utf-16be`、`gb18030` 和 `big5`。

格式错误的输入会被拒绝。旧编码解码路径还会拒绝包含替换字符的解码结果，
避免静默转换已损坏的字节。不进行基于统计的编码猜测。

## 实验性 HDR10/PQ 调色板

默认输出 SDR。若要使用 BT.2020 原色和 ST 2084 PQ 编码调色板颜色：

```sh
pgs-converter convert movie.ass \
  --size 3840x2160 --fps 24000/1001 \
  --color-profile hdr10 --subtitle-white-nits 203 \
  -o movie.hdr.sup
```

HDR 模式下，`--subtitle-white-nits` 默认为 **203 nits**，
接受大于 0 且不超过 10000 的有限数值。SDR 模式不允许显式指定该参数。
高于 300 nits 时会提示观看舒适性风险。
参考白设置表达的是**编码意图**，不保证屏幕实测亮度。HDR 与画布分辨率无关。

HDR 管线将源字幕 RGB 解释为 SDR sRGB/BT.709 图形，先线性化，
再将原色转换为 BT.2020，按参考白亮度缩放，应用 PQ，
最终写入有限范围的 8 位 YCbCr 调色板条目。
它不是简单切换 YCbCr 矩阵，也不会通过缩放透明度模拟亮度。

调色板选择使用确定性的线性光、预乘透明度 RGBA 特征；
PQ 变换以浮点计算完成，之后才进行最终调色板编码。
源渲染器仍使用 libass 的 8 位覆盖率和合成机制。

重要限制：

- 独立 SUP 没有可通用依赖的原色、传递函数或参考白亮度信令。
  播放器可能假设 BT.709、从视频猜测颜色属性，或无视调色板意图而按 SDR 合成字幕。
- **HDR 选项是实验性的。**目前尚未选定并验证目标 HDR 播放器及输出链路。
  解码器能够读取文件，不代表 HDR 颜色正确。
  播放器和显示设备设置还可能重新映射亮度。
- 不生成或修改 HDR10 视频元数据、MaxCLL/MaxFALL，不提供 HLG、
  Dolby Vision 专用处理、视频色调映射或自动视频参数探测。
- 同时需要 SDR 和 HDR 交付时，应分别生成两份 SUP。
  4K 分辨率和 `.hdr.sup` 文件名都不能替代 HDR 信令。

## 渲染与格式限制

字幕按指定帧率的全局帧网格采样，使用整数和有理数计时；
字幕边界取整误差最多为一帧。
完全落在采样帧之间的短事件会被报告，严格模式下会被拒绝。
像素与位置均未变化的画面会被去重，空白区间会被跳过，
并通过显式空组合在字幕间隔和结尾清除画面。

PGS 最多支持 256 个调色板条目。编码器保留透明条目；
颜色数量足够少时，中间 RGB 调色板精确保留源颜色；
超出时使用确定性的加权中位切分量化。
即使不缩减调色板颜色数量，最终 YCbCr 编码仍可能有损。

动画或大面积字幕可能产生较大的文件。
每个变化画面都是完整、自包含的显示组合，尚未使用高级调色板或对象缓存复用。
帧按流式处理，不会将整部影片的所有字幕帧保存在内存中。

SDR 输出使用 BT.709 有限范围调色板转换。
部分解码器对高度不超过 576 行的画布假设使用 BT.601，工具会对此给出警告。
可选 HDR 转换之前，ASS 的 `YCbCr Matrix` 兼容处理以 BT.709 SDR 为渲染目标。
`None` 表示禁用该兼容处理；缺少此头部时遵循传统的 TV.601 解释。
这不会推断视频颜色属性，也不保证与未知视频精确匹配颜色。

不接受负时间、非正持续时间和超出 PGS 字段范围的数值。
不支持 32 位、90 kHz 时间戳回绕，即时间轴范围约为 13.25 小时。
动画保真度受帧率、调色板量化、libass 兼容性和播放器行为限制。
严格蓝光缓冲区及带宽合规、实体播放器认证不在当前范围内。

## 开发与验证

```sh
go test ./...
go vet ./...
go test -race ./...       # 需要主机及工具链支持
go test ./internal/pgs -run '^$' -fuzz FuzzRLE -fuzztime 5s
go test ./internal/subtitle -run '^$' -fuzz FuzzDecode -fuzztime 5s
```

安装 FFmpeg 后会启用独立 PGS 解码器集成测试，否则该测试会报告跳过。
测试通过 `-copyts` 保留原始时间戳，将解码后的 HD 帧与黑色背景上的
libass 源帧比较，并检查空白帧是否完全没有字幕残留。
活动像素平均 RGB 分量误差上限为 2，峰值分量误差上限为 16。
字体是否可用可能影响渲染结果。

其他测试覆盖毫秒时间、SSA 输入、字面文本转义、事件重叠、RLE 边界、
ODS 分片、确定性量化、输出保护、取消和部分成功的批处理。

HDR 测试验证 ST 2084 参考数值、原色转换、调色板黑白点、
实际编码的 203 nits 白色，以及切换色彩配置时几何与透明度保持不变。
这些测试不认证播放器的 HDR 解释行为。
对于固定渐变样例，量化测试要求线性光预乘分量的平均误差不超过 0.025、
峰值不超过 0.15；这些阈值不适用于任意输入。

### macOS 便携包

```sh
go build -trimpath -o build/pgs-converter ./cmd/pgs-converter
bash scripts/bundle-macos.sh build/pgs-converter dist/pgs-converter-macos
```

脚本会复制传递依赖的 Homebrew dylib、重写加载路径、应用 ad-hoc 签名，
并包含依赖许可证说明和构建版本记录。
它会从匹配的上游源码标签下载 FreeType 许可证全文，因此打包需要联网。
这不等于通过 Apple 公证。

Windows 配套脚本会沿 DLL 导入关系收集依赖，
并复制所选 MSYS2 环境的许可证目录。
公开再分发压缩包前，应参阅 [THIRD_PARTY_NOTICES](./THIRD_PARTY_NOTICES)，
审核原生依赖许可证、对应源码提供及重新链接等义务。

正式发布前仍需完成：四个目标平台的干净环境测试、原生依赖源码和版本锁定、
实际播放器冒烟测试、长时间运行性能评估，以及目标 HDR 播放链路验证。

本机 macOS ARM64 在 1920×1080、24 fps 下的小型合成样例实测：

| 样例 | 显示集数量 | 输出大小 | 峰值 RSS |
| --- | --- | --- | --- |
| 一小时静态字幕 | 2 | 约 11 KiB | 约 22 MiB |
| 一分钟移动 HDR 字幕 | 1,441 | 约 11 MiB | 约 29 MiB |

这些数据仅代表小型合成样例，不是通用性能保证。
独立 FFmpeg MKV 封装/解封装往返已经过测试；
实际目标播放器的显示行为仍属于发布前验收项目。
