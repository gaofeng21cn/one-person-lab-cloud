# 规格验证与证据

此目录只检查文档/契约，不访问现有数据库、云资源或生产网络。

## 复验

```sh
python3 -m venv /tmp/opl-spec-validation
/tmp/opl-spec-validation/bin/python -m pip install -r checks/requirements.txt
/tmp/opl-spec-validation/bin/python checks/render_traceability.py
/tmp/opl-spec-validation/bin/python checks/validate_spec.py
```

本轮对齐REST/proto/events权威规格后，先运行同仓生成器与字段检查，再按现有隔离PostgreSQL脚本重取绑定新hash的cross-domain回执；旧原型回执仅证明旧OpenAPI字节，handoff仍会报告`needs_correction`，须对当前字节重跑受影响UI原型后才能更新证据。生成绑定和六服务真实消费者尚需W01/W02完成，不能因规格静态PASS称它们已接入。依赖锁定为本次实际使用版本，不要求更新到latest。脚本在失败时返回非零，并追加checks/runs/<UTC时间>.json；verification.json仅是最新结果索引，不覆盖此前失败记录。

## 文件

- validate_spec.py：OpenAPI/重复键/owner/幂等/CSRF、SQL AST/字段类型/约束边界、API字段来源、UI操作/字段、功能矩阵、proto编译、事件、正反例。
- contract_cases.json：明确设计的REST正例/拒绝反例，直接验证03 schema，不复述另一套DTO。
- event_cases.json：事件payload、owner/租户、int64边界和秘密拒绝反例。
- render_traceability.py：从API/DB/UI生成10和traceability.json，不新增产品权威。
- db_execution.json：初次57项数据库执行证据，保留旧schema哈希，不代表最终schema。
- db_execution_subscription_alignment.json：最终69项实测，包含当前schema/inventory哈希、PostgreSQL版本、镜像digest、重放脚本及容器清理事实。
- verification.json和runs/：最新索引及不可变逐次规格检查。失败记录是诊断历史，不是最终通过证据。

DDL测试确实在隔离postgres:16-alpine容器中执行，使用--network none及tmpfs，不挂用户目录/持久卷；本目录静态脚本只核验其receipt与当前DDL哈希，不重新启动数据库。重放脚本路径记录在对应receipt中，交付包内携带的重放内容见receipt本身的replayScripts字段。

数据库约束不证明应用事务、资金/资源外部原子性、Saga恢复、真实浏览器链路、数据实际迁移或Instance发布资格。这些实施接受标准在08/09，不得以本次通过替代。

## 全量交接收口复验

```sh
/tmp/opl-spec-validation/bin/python checks/render_domain_flows.py
/tmp/opl-spec-validation/bin/python checks/validate_contract_semantics.py
python3 checks/validate_publisher_source.py --cloud-repo /absolute/path/to/opl-cloud
# validate_cross_domain.py的参数与隔离DB运行方式以其--help/文件说明为准；会创建并清理自身network-none/tmpfs容器。
/tmp/opl-spec-validation/bin/python checks/render_traceability.py
/tmp/opl-spec-validation/bin/python checks/validate_spec.py
/tmp/opl-spec-validation/bin/python checks/validate_handoff.py
```

- 最新cross-domain-*.json证明同一API/protobuf输入穿过Owner映射、SQL约束和读回投影，不再仅测字段是否存在；报告带输入hash及清理结果。
- publisher-source-*.json使用当前真实Cloud Go decoder/validator/startup order，不仅验证自写JSON Schema；测试不会改产品源码。
- ui_prototype_verification.json及ui-prototype/screenshots证明离线原型的可点击行为、窄屏/焦点/页面状态，不证明服务已实现。
- handoff_readiness.json合并以上证据并单独处理D17。pending_user不能被任一绿色测试覆盖；status=awaiting_business_decision时不得称全量READY。
- 最终接口、表数、用例数以最新证据的实际结果为准，不以先前固定计数判断是否完成。

## D17已批准后的完整复验

新增`13_plan_change_policy.md`与固定`contracts/plan-change-policy.json`是已确认规则，不再是未决选项。

```sh
/tmp/opl-spec-validation/bin/python checks/validate_d17_contract.py
go run checks/d17_native_millis.go
```

`d17_acceptance_vectors.json`保存独立数学期望。最新cross-domain回执还实际验证了单行补差明细、下期目标价、原E、精确毫秒、未付款取消重绑定、并发唯一原单、失败与未用区间退款；不是仅把approved字段改成true。原`pending`记录保留为历史，当前结论读handoff_readiness.json。

## 完整开发执行计划

```sh
python3 checks/render_development_plan.py
python3 checks/validate_development_plan.py
python3 -m unittest discover -s checks -p 'test_development_plan.py'
```

14与development_plan.json是同源任务书，不重复定义业务DTO。检查涵盖全部F/API/Owner表/内部RPC、现有代码路径、拟建位置和启动/验收依赖无环；同时拒绝Cloud领域落点逃出当前仓库、tenant/Gateway误拆模块和非Instance任务越界写入。生成器从当前checkout确定工作根；历史来源SHA和旧回执保持原样。只说明任务覆盖和计划可执行，不执行W00–W31的实现、部署或收费。

既有协议/SQL/UI字节未改时复用其精确哈希证据，不为增加任务书重跑有副作用的资格流程。打包/签收忽略.DS_Store、__MACOSX、__pycache__及.pyc，这些是工作站元数据而不是产品规则。


## 字段与领域对齐（2026-09-23）

先读`../15_domain_alignment.md`。`../reference/`是可再生成的只读视图，原02/03/proto/events仍是字段Owner。每域第1节是人工核对的职责说明；第2节起由生成器从规范派生，不手工修改派生字段。

```sh
# 指定本机已安装requirements.txt的Python；输出必须是新文件，不覆盖旧receipt。
python checks/render_domain_reference.py --report /tmp/domain-reference-new.json
python checks/validate_domain_reference.py
# 当前已知ALIGN-01–04会使上一个命令非零；只生成审计报告可用--allow-known-gaps，报告仍passed=false。
python -m unittest discover -s checks -p 'test_domain_reference.py'
```

旧代码API注册和JSON类型按冻结SHA的Go AST抽取：

```sh
go run checks/extract_legacy_surface.go /absolute/path/to/frozen-baseline > /tmp/legacy-surface.json
python checks/render_domain_reference.py --legacy-source-json /tmp/legacy-surface.json --report /tmp/domain-reference-with-baseline-new.json
```

旧DB不是解析展示SQL得到：在`git archive 50520e27 services packages`的隔离副本中，用`legacy_schema_probe_test.go.tmpl`调用各Owner真实loader，临时PostgreSQL16空库安装后读回系统目录。模板的`OWNERPACKAGE/INSTALL`替换分别是server/newTestPostgresEntStateStore、fabric/newTestPostgresOperationStore、ledger/NewPostgresStore(db).Install；准确调用记录在`runs/legacy-schema-audit-20260923.json`。仅修改副本、不触碰原源码或生产库，销毁本次容器。此读回不涵盖有客户数据的迁移。

`validate_handoff.py`现在合并字段对齐回执，未关闭的跨Owner契约缺口不能被旧的静态PASS覆盖。更新任何绑定源后先运行计划/字段校验，再跑validate_spec，最后validate_handoff；保留失败记录，不通过修改回执假装完成。
