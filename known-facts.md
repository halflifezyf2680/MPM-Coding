# Known Facts

| 状态 | 类型 | 内容 | scope |
|---|---|---|---|
| active | success_pattern | MCP工具参数设计：当LLM容易填错多个参数时，合并为单参数+内部自动识别，而非依赖提示词引导 | project |
| active | success_pattern | Windows上Edit工具编辑含中文+tab缩进的多行Go raw string时容易匹配失败，应改用Python脚本 | project |
| active | success_pattern | 工具描述应精简：代码内部已自动处理的分支逻辑不需要在描述中向LLM解释 | project |
| active | success_pattern | 版本发布流程：git tag v2.x.0 → git push origin v2.x.0 触发 Action 构建 → gh release edit v2.x.0 --notes 补充升级说明（新特性/修复/下载列表） | project |
| active | success_pattern | Release Notes必须包含实际升级说明（新特性+修复），不能只有模板。v2.5.0起开始有，之前版本都是空模板 | project |
| active | success_pattern | 在 Python argparse dispatch 场景下，单纯抓字面 callee 不足以形成可靠 call edge，必须同时捕获 keyword_argument 注册信息并在索引阶段做局部绑定解析。 | project |
| active | success_pattern | 当现有测试只验证基础 call 时，新增 dispatch 测试更能防止 call edge 回归到只识别直连调用。 | project |
