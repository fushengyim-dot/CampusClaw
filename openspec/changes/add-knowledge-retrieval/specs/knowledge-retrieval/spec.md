# knowledge-retrieval Specification

本文依据公开第4课实现教程编写；教师教程所指的原始`week04/knowledge-retrieval` spec尚未取得。若取得原文，须先逐项核对本规格再标记最终验收。

## ADDED Requirements

### Requirement: R01 Preserve source text and create chunks

系统 MUST 保留原材料和knowledge entry原文，并为可检索材料创建关联到班级、材料和原文条目的切片。切片 MUST 保存序号、正文和切分输入中的字符区间；重新切片 MUST NOT 改写原文。

#### Scenario: S01 Default automatic chunking
- **WHEN** 材料以默认策略创建切片
- **THEN** 每片 MUST 不超过约800字符、相邻片重叠80字符，并优先在空行、换行或句号处断开
- **AND** 原材料字节和knowledge entry原文 MUST 保持不变

#### Scenario: S02 Custom chunking
- **WHEN** 教师提供custom策略、分隔符、最大长度和重叠比例
- **THEN** 长度 MUST 在100—2000字符范围内，重叠比例MUST在0%—50%范围内，并按指定分隔符切分
- **AND** 无可用断点时MUST按最大长度切开

#### Scenario: S03 Markdown hierarchy chunking
- **WHEN** 教师选择hierarchy策略处理Markdown
- **THEN** 切分MUST按标题层级分章并保留标题；超长章节MUST再按自动窗口切分

### Requirement: R02 Class-scoped traceable retrieval

keyword、vector和hybrid检索 MUST 只使用服务端会话中的class_id；请求参数中的班级编号 MUST NOT 改变范围。结果 MUST 从MySQL取回正文并给出材料标题、切片序号、字符区间、摘录和原材料入口。

#### Scenario: S04 Keyword retrieval
- **WHEN** 用户选择keyword模式
- **THEN** 系统 MUST 使用MySQL ngram全文索引按相关度排序，MUST NOT调用嵌入服务或Qdrant
- **AND** 返回切片 MUST 全部属于当前会话班级且索引状态可用

本地实现中，`ready`表示切片文本已生成且MySQL全文索引可查询；向量是否已写入Qdrant由向量存储单独判断。公开教程提到嵌入失败状态，但原始规格不可取得，故需在取得原文后核对该状态定义。

#### Scenario: S05 Vector retrieval
- **WHEN** 用户选择vector模式
- **THEN** 系统 MUST 将问句嵌入后按Qdrant余弦相似度检索并应用会话班级过滤
- **AND** 低于0.35的候选MUST丢弃，系统MUST按切片ID回MySQL并再次过滤班级后取得正文

#### Scenario: S06 Hybrid retrieval
- **WHEN** 用户选择hybrid模式
- **THEN** 系统 MUST 同时执行keyword和vector路径，分别筛选后按RRF(k=60)融合名次
- **AND** MUST NOT把不同路径的原始相关分数直接相加；缺席的一路不贡献名次分

#### Scenario: S07 Cross-class query
- **WHEN** 用户以A班会话检索只存在于B班材料的内容，或伪造B班class_id
- **THEN** 系统 MUST 返回HTTP 200和空命中结果，不泄露B班切片或其存在性

#### Scenario: S08 Dependency failure
- **WHEN** Qdrant不可用
- **THEN** keyword模式MUST仍能检索；vector和hybrid MUST返回503，不伪造相似度或命中结果

### Requirement: R03 Evidence-gated answers

系统 MUST 仅将当前会话班级的已命中切片传给对话服务。回答MUST用与citations顺序一致的`[n]`标注出处。

#### Scenario: S09 No evidence
- **WHEN** 本班hybrid检索无命中
- **THEN** 系统 MUST 返回“资料中未找到相关内容”及空citations
- **AND** 系统 MUST NOT调用对话服务

#### Scenario: S10 Answer with citations
- **WHEN** hybrid检索命中切片
- **THEN** 系统 MUST 最多将前4条切片和最新问题发送给生成模型，并返回简短答案及可打开的出处
- **AND** 客户端system消息、向量分量和其他班资料 MUST NOT进入生成请求

#### Scenario: S11 Provider configuration and failure
- **WHEN** 外部服务配置缺失或请求失败
- **THEN** 错误 MUST 脱敏、有限时返回且 MUST NOT暴露服务端凭据
- **AND** 未获授权时系统 MUST NOT发起真实外部嵌入或生成调用

### Requirement: R04 Explicit and repeat-safe vector indexing

系统 MUST 在用户上传讲义时只生成本地切片；不得自动把讲义内容发送到外部嵌入服务。只有同班教师明确启动向量索引后，后端才可将该材料的切片发给嵌入服务。向量状态 MUST 与关键词切片状态分开记录，并用切片正文SHA-256判断内容是否变化；未变化且已就绪的切片 MUST 跳过。向量检索 MUST 只返回向量状态为ready且内容指纹仍匹配的切片。

#### Scenario: S12 Teacher explicitly indexes a material
- **WHEN** 同班教师在材料页面启动向量索引
- **THEN** 后端 MUST 使用会话class_id复核材料归属，单次最多处理10个待索引切片，成功后保存向量和状态；失败 MUST 脱敏并停止，不自动重试
- **AND** 相同内容再次索引 MUST 复用已就绪向量；学生或其他班级教师 MUST NOT 启动或读取跨班索引状态
