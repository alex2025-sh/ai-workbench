-- 平台表结构（MySQL 8.0 / TiDB 兼容），设计见 docs/ai-task-platform.md。
-- goctl 约束：单列主键、AUTO_INCREMENT；TiDB 专用选项写在 /*T! ... */ 注释中；created_at / updated_at 由数据库维护。

-- 任务类型：一个 key 一行，带当前草稿
CREATE TABLE `task_type` (
  `id`               bigint unsigned NOT NULL AUTO_INCREMENT,
  `type_key`         varchar(64)  NOT NULL,
  `namespace`        varchar(64)  NOT NULL DEFAULT '',
  `name`             varchar(128) NOT NULL,
  `latest_version`   int          NOT NULL DEFAULT 0 COMMENT '0 = 尚未发布',
  `draft`            json         NULL COMMENT '草稿（任务类型 JSON）；NULL = 没有草稿',
  `draft_revision`   bigint       NOT NULL DEFAULT 0 COMMENT '草稿乐观锁',
  `draft_updated_by` varchar(64)  NOT NULL DEFAULT '',
  `created_by`       varchar(64)  NOT NULL DEFAULT '',
  `created_at`       datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`       datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_type_key` (`type_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='任务类型与草稿';

-- 任务类型的已发布版本，不可变；任务创建时固定版本
CREATE TABLE `task_type_version` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `type_key`     varchar(64)  NOT NULL,
  `version`      int          NOT NULL,
  `body`         json         NOT NULL COMMENT '任务类型 JSON',
  `content_hash` varchar(64)  NOT NULL,
  `published_by` varchar(64)  NOT NULL,
  `published_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_key_version` (`type_key`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='任务类型的已发布版本';

-- 任务：任务行就是流程状态的唯一事实来源
CREATE TABLE `task` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_type`    varchar(64)  NOT NULL,
  `type_version` int          NOT NULL COMMENT '创建时的版本，节点链按它走完',
  `biz_key`      varchar(191) NOT NULL COMMENT '数据源给出的 key',
  `track_key`    varchar(191) NULL COMMENT '跟踪中 = biz_key，否则 NULL；同一类型同一 key 只有一个跟踪中的任务',
  `fields`       json         NOT NULL COMMENT '业务字段，数据源每次出现时刷新',
  `node_id`      varchar(64)  NOT NULL COMMENT '当前人工节点',
  `status`       smallint     NOT NULL COMMENT '-1 unassigned / 0 pending / 1 processing / 2 snoozed / 3 solved',
  `priority`     smallint     NOT NULL DEFAULT 2 COMMENT '0 P0 ... 3 P3',
  `assignee_id`  varchar(64)  NOT NULL DEFAULT '' COMMENT '当前处理人；待认领时为空',
  `snooze_until` datetime(3)  NULL,
  `remind_at`    datetime(3)  NULL COMMENT '待认领时再次提醒群的时间（节点配置了 remind_after），提醒一次后清空',
  `resolution`   varchar(32)  NOT NULL DEFAULT '' COMMENT 'handled / no_response / auto_closed',
  `source_at`    bigint       NOT NULL DEFAULT 0 COMMENT '推送来源的 occurred_at（毫秒），丢弃更旧的消息',
  `version`      bigint       NOT NULL DEFAULT 0 COMMENT '每次状态转移加一',
  `closed_at`    datetime(3)  NULL,
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_track` (`task_type`, `track_key`),
  KEY `idx_assignee` (`assignee_id`, `status`, `priority`),
  KEY `idx_snooze` (`status`, `snooze_until`),
  KEY `idx_remind` (`status`, `remind_at`),
  KEY `idx_type_status` (`task_type`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='任务';

-- 任务历史：每一次状态转移一行，详情页时间线用
CREATE TABLE `task_event` (
  `id`          bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id`     bigint unsigned NOT NULL,
  `kind`        varchar(32)  NOT NULL COMMENT 'created / started / close / no_response / snooze / escalate / woke / gone / reassigned / priority',
  `actor`       varchar(64)  NOT NULL DEFAULT '' COMMENT '操作人；系统为 system',
  `node_id`     varchar(64)  NOT NULL DEFAULT '',
  `from_status` smallint     NOT NULL,
  `to_status`   smallint     NOT NULL,
  `detail`      json         NULL,
  `created_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  KEY `idx_task` (`task_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='任务历史（只追加）';

-- 经手人：处理过任务的人，转走或升级后仍能只读查看
CREATE TABLE `task_participant` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id`    bigint unsigned NOT NULL,
  `user_id`    varchar(64)  NOT NULL,
  `created_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_task_user` (`task_id`, `user_id`),
  KEY `idx_user` (`user_id`, `task_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='任务经手人';

-- 待投递的外部动作（Lark 通知）：与状态转移同一事务写入，提交后投递，失败由 ticker 重试
CREATE TABLE `outbox` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id`    bigint unsigned NOT NULL,
  `kind`       varchar(32)  NOT NULL COMMENT 'lark_user',
  `target`     varchar(128) NOT NULL COMMENT '接收人（用户 id）',
  `payload`    json         NOT NULL COMMENT '渲染好的卡片',
  `idem_key`   varchar(191) NOT NULL,
  `status`     tinyint      NOT NULL DEFAULT 0 COMMENT '0 待投递 / 1 已投递 / 2 放弃',
  `attempts`   int          NOT NULL DEFAULT 0,
  `next_at`    datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `last_error` varchar(512) NOT NULL DEFAULT '',
  `created_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_idem_key` (`idem_key`),
  KEY `idx_due` (`status`, `next_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='外部动作发件箱';

-- 数据源的调度状态与上一次结果（SQL 来源）
CREATE TABLE `source_state` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_type`    varchar(64)  NOT NULL,
  `next_run_at`  datetime(3)  NOT NULL,
  `lease_until`  datetime(3)  NULL COMMENT '正在执行的实例持有到此时；过期视为可重新执行',
  `last_run_at`  datetime(3)  NULL,
  `last_rows`    int          NOT NULL DEFAULT 0,
  `last_created` int          NOT NULL DEFAULT 0,
  `last_closed`  int          NOT NULL DEFAULT 0,
  `last_error`   varchar(512) NOT NULL DEFAULT '',
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_task_type` (`task_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='数据源调度状态';
