-- 任务中心表结构（TiDB / MySQL 兼容）
-- 设计见 docs/ai-task-platform.md「核心概念」「任务状态」「任务优先级」。
-- goctl 约束同 engine.sql：单列主键、AUTO_INCREMENT、created_at / updated_at 由数据库维护。

-- 任务实例：公共列 + JSON payload
CREATE TABLE `task` (
  `id`               bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_type`        varchar(64)  NOT NULL,
  `type_version`     int          NOT NULL COMMENT '绑定 payload schema 版本',
  `subject_type`     varchar(32)  NOT NULL COMMENT 'vip / ticket / account ...',
  `subject_id`       varchar(64)  NOT NULL,
  `status`           smallint     NOT NULL COMMENT '-1 unassigned / 0 pending / 1 processing / 2 snoozed / 3 solved（>=3 终态）',
  `priority`         smallint     NOT NULL DEFAULT 2 COMMENT '0 P0 立即 / 1 P1 今日 / 2 P2 机会 / 3 P3 维护',
  `assignee_id`      varchar(64)  NOT NULL DEFAULT '' COMMENT '当前处理人；空 = 待分配',
  `assignee_group`   varchar(64)  NOT NULL DEFAULT '' COMMENT '组队列，认领前使用',
  `current_node`     varchar(64)  NOT NULL DEFAULT '' COMMENT '当前所处的人工节点',
  `run_id`           bigint unsigned NOT NULL DEFAULT 0 COMMENT '驱动该任务的 workflow run',
  `resolution`       varchar(32)  NOT NULL DEFAULT '' COMMENT 'contacted / dismissed / escalated_out / auto_closed',
  `sla_status`       varchar(16)  NOT NULL DEFAULT 'on_track' COMMENT 'on_track / at_risk / breached',
  `escalation_level` tinyint      NOT NULL DEFAULT 0,
  `snooze_until`     datetime(3)  NULL COMMENT 'snoozed 状态的回迁时间',
  `due_at`           datetime(3)  NULL,
  `dedup_key`        varchar(191) NOT NULL COMMENT '如 ticket_id:48h，防止重复建任务',
  `payload`          json         NOT NULL COMMENT '业务字段，按 task_type@type_version 的 schema 校验',
  `seq`              bigint       NOT NULL DEFAULT 0 COMMENT '引擎同步序号，只接受更大的 seq',
  `version`          bigint       NOT NULL DEFAULT 0 COMMENT '乐观锁',
  `created_at`       datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`       datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_dedup_key` (`dedup_key`),
  KEY `idx_assignee_status` (`assignee_id`, `status`, `priority`),
  KEY `idx_group_status` (`assignee_group`, `status`),
  KEY `idx_subject` (`subject_type`, `subject_id`),
  KEY `idx_status_snooze` (`status`, `snooze_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='任务实例';

-- 任务事件：每一次变更（状态、处理人、优先级、回执），审计与统计的数据来源
CREATE TABLE `task_event` (
  `id`          bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id`     bigint unsigned NOT NULL,
  `type`        varchar(32)  NOT NULL COMMENT 'status_changed / reassigned / priority_changed / claimed / summary_failed ...',
  `from_status` smallint     NULL,
  `to_status`   smallint     NULL,
  `actor`       varchar(64)  NOT NULL DEFAULT '' COMMENT '操作人；引擎同步为 engine',
  `reason`      varchar(512) NOT NULL DEFAULT '',
  `detail`      json         NULL,
  `created_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  KEY `idx_task_created` (`task_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='任务事件（只追加）';
