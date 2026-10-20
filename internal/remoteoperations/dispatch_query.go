package remoteoperations

// 过滤当前不再允许的候选，避免队首失效策略或平台任务阻塞该节点其他工作。
const dispatchAdmissionSQL = ` AND p.enabled=1 AND json_extract(p.configuration_json,'$.enabled')=1
 AND json_extract(c.configuration_json,'$.enabled')=1
 AND t.kind IN (SELECT value FROM json_each(p.configuration_json,'$.kinds'))
 AND t.kind IN (SELECT value FROM json_each(c.configuration_json,'$.kinds'))
 AND t.timeout_seconds<=json_extract(p.configuration_json,'$.timeout_seconds') AND t.timeout_seconds<=json_extract(c.configuration_json,'$.timeout_seconds')
 AND t.output_bytes<=json_extract(p.configuration_json,'$.output_bytes') AND t.output_bytes<=json_extract(c.configuration_json,'$.output_bytes')
 AND (EXISTS(SELECT 1 FROM operation_policy_nodes pn WHERE pn.policy_id=p.id AND pn.node_id=n.node_id)
 OR EXISTS(SELECT 1 FROM operation_policy_groups pg JOIN nodes nn ON nn.group_id=pg.group_id WHERE pg.policy_id=p.id AND nn.id=n.node_id))
 AND (t.kind IN ('deploy','collect') OR (COALESCE(json_extract(t.policy_snapshot,'$.task_shell_family'),'sh')
 IN (SELECT value FROM json_each(c.configuration_json,'$.supported_shells'))
 AND (COALESCE(json_array_length(p.configuration_json,'$.supported_shells'),0)=0 OR
 COALESCE(json_extract(t.policy_snapshot,'$.task_shell_family'),'sh') IN (SELECT value FROM json_each(p.configuration_json,'$.supported_shells')))))
 AND (COALESCE(json_extract(t.policy_snapshot,'$.task_run_as_root'),0)=0 OR
 (json_extract(p.configuration_json,'$.allow_root')=1 AND json_extract(c.configuration_json,'$.allow_root')=1))
 AND (t.kind NOT IN ('deploy','collect') OR (
 EXISTS(SELECT 1 FROM json_each(p.configuration_json,'$.roots') r WHERE json_extract(r.value,'$.id')=f.root_id
 AND f.byte_size<=json_extract(r.value,'$.maximum_bytes') AND
 ((t.kind='collect' AND json_extract(r.value,'$.read')=1) OR (t.kind='deploy' AND json_extract(r.value,'$.write')=1
 AND (f.overwrite=0 OR json_extract(r.value,'$.overwrite')=1) AND (f.mode=384 OR json_extract(r.value,'$.executable')=1))))
 AND EXISTS(SELECT 1 FROM json_each(c.configuration_json,'$.roots') r WHERE json_extract(r.value,'$.id')=f.root_id
 AND f.byte_size<=json_extract(r.value,'$.maximum_bytes') AND
 ((t.kind='collect' AND json_extract(r.value,'$.read')=1) OR (t.kind='deploy' AND json_extract(r.value,'$.write')=1
 AND (f.overwrite=0 OR json_extract(r.value,'$.overwrite')=1) AND (f.mode=384 OR json_extract(r.value,'$.executable')=1))))))`
