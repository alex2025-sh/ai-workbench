-- 轻量工作流引擎表结构（TiDB / MySQL 兼容）
-- 设计见 docs/workflow-engine.md「数据模型」。
--
-- goctl 约束（goctl model mysql ddl）：
--   * 每张表必须有单列主键，联合主键改为 id 主键 + 联合唯一键（goctl 会生成 FindOneByXxx）。
--   * 主键使用 AUTO_INCREMENT，goctl 生成的 Insert 会省略该列。
--     TiDB 上用 NONCLUSTERED 主键 + SHARD_ROW_ID_BITS 打散行数据写入，缓解自增热点；
--     /*T! ... */ 是 TiDB 专用注释，MySQL 与 goctl 解析时会忽略。
--   * created_at / updated_at 由 goctl 自动忽略，交给数据库默认值维护。

-- 编译后的执行计划，发布后不可变
CREATE TABLE `wf_plan` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `plan_id`      varchar(64)  NOT NULL COMMENT 'workflow 定义 ID',
  `version`      int          NOT NULL COMMENT '发布版本，不可变',
  `dsl`          json         NOT NULL COMMENT '原始图模型 DSL',
  `compiled`     json         NOT NULL COMMENT '编译产物 Plan',
  `checksum`     varchar(64)  NOT NULL DEFAULT '',
  `published_by` varchar(64)  NOT NULL DEFAULT '',
  `published_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_plan_version` (`plan_id`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='workflow 执行计划';

-- 运行实例
CREATE TABLE `wf_run` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `plan_id`      varchar(64)  NOT NULL,
  `plan_version` int          NOT NULL COMMENT '启动时锁定，之后不变',
  `biz_key`      varchar(128) NOT NULL COMMENT '业务幂等键，如 task:<task_id>',
  `status`       varchar(32)  NOT NULL COMMENT 'running / succeeded / failed / needs_intervention / cancelled',
  `vars`         json         NOT NULL COMMENT '流程变量与触发数据引用',
  `version`      bigint       NOT NULL DEFAULT 0 COMMENT '每次推进 +1；同时作为 sync_task 的 seq',
  `exec_count`   int          NOT NULL DEFAULT 0 COMMENT '节点总执行次数，用于防死循环上限',
  `dry_run`      tinyint      NOT NULL DEFAULT 0 COMMENT '1 = 试运行实例',
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_biz_key` (`biz_key`),
  KEY `idx_status_updated` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='workflow 运行实例';

-- 节点的当前状态，每个节点一行（只保存当前轮）
CREATE TABLE `wf_node_run` (
  `id`          bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id`      bigint unsigned NOT NULL,
  `node_id`     varchar(64)  NOT NULL,
  `iteration`   int          NOT NULL DEFAULT 0 COMMENT 'v1 恒为 0，为受控的环预留',
  `status`      varchar(32)  NOT NULL COMMENT 'pending / ready / running / waiting / succeeded / skipped / failed',
  `port`        varchar(64)  NOT NULL DEFAULT '' COMMENT '完成时选中的输出端口',
  `output`      json         NULL COMMENT '节点输出；大输出只存引用',
  `attempt`     int          NOT NULL DEFAULT 0,
  `wait_until`  datetime(3)  NULL,
  `started_at`  datetime(3)  NULL,
  `finished_at` datetime(3)  NULL,
  `created_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_run_node` (`run_id`, `node_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='节点当前状态';

-- 环重置时归档旧轮次，用于审计与画布回放
CREATE TABLE `wf_node_run_history` (
  `id`          bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id`      bigint unsigned NOT NULL,
  `node_id`     varchar(64)  NOT NULL,
  `iteration`   int          NOT NULL,
  `status`      varchar(32)  NOT NULL,
  `port`        varchar(64)  NOT NULL DEFAULT '',
  `output`      json         NULL,
  `attempt`     int          NOT NULL DEFAULT 0,
  `started_at`  datetime(3)  NULL,
  `finished_at` datetime(3)  NULL,
  `created_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_run_node_iter` (`run_id`, `node_id`, `iteration`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='节点历史轮次';

-- 统一任务表：立即执行、超时、唤醒、定时器、任务状态同步都是 job
CREATE TABLE `wf_job` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id`       bigint unsigned NOT NULL,
  `node_id`      varchar(64)  NOT NULL DEFAULT '',
  `iteration`    int          NOT NULL DEFAULT 0,
  `kind`         varchar(16)  NOT NULL COMMENT 'execute / timeout / wake / timer / sync_task',
  `payload`      json         NULL COMMENT 'sync_task 等 job 的参数',
  `due_at`       datetime(3)  NOT NULL,
  `status`       varchar(16)  NOT NULL COMMENT 'pending / done / cancelled',
  `attempt`      int          NOT NULL DEFAULT 0,
  `owner`        varchar(64)  NOT NULL DEFAULT '' COMMENT '租约持有者，空 = 未认领',
  `locked_until` datetime(3)  NULL COMMENT '租约到期时间',
  `last_error`   varchar(1024) NOT NULL DEFAULT '',
  `idem_key`     varchar(191) NOT NULL COMMENT 'run_id:node_id:iteration:kind[:seq]',
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_idem_key` (`idem_key`),
  KEY `idx_status_due` (`status`, `due_at`),
  KEY `idx_run` (`run_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='引擎 job';

-- 退回边（rework）的计数
CREATE TABLE `wf_loop_counter` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id`     bigint unsigned NOT NULL,
  `edge_id`    varchar(64)  NOT NULL,
  `count`      int          NOT NULL DEFAULT 0,
  `updated_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  UNIQUE KEY `uk_run_edge` (`run_id`, `edge_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='退回次数';

-- cron 来源，由 ticker 计算并推进 next_fire_at
CREATE TABLE `wf_schedule` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `name`         varchar(128) NOT NULL,
  `cron`         varchar(64)  NOT NULL COMMENT '5 段 cron 表达式',
  `timezone`     varchar(64)  NOT NULL DEFAULT 'Asia/Kuala_Lumpur',
  `target`       json         NOT NULL COMMENT '触发目标：扫描 skill + 参数 + 事件映射，或直接启动的 plan',
  `next_fire_at` datetime(3)  NOT NULL,
  `enabled`      tinyint      NOT NULL DEFAULT 1,
  `version`      bigint       NOT NULL DEFAULT 0 COMMENT '乐观锁，防止多实例重复触发',
  `created_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`   datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_name` (`name`),
  KEY `idx_enabled_next` (`enabled`, `next_fire_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='定时来源';

-- 追加写的事件日志：状态变更、信号、人工干预
CREATE TABLE `wf_event` (
  `id`         bigint unsigned NOT NULL AUTO_INCREMENT,
  `run_id`     bigint unsigned NOT NULL,
  `node_id`    varchar(64)  NOT NULL DEFAULT '',
  `iteration`  int          NOT NULL DEFAULT 0,
  `type`       varchar(64)  NOT NULL,
  `payload`    json         NULL,
  `actor`      varchar(64)  NOT NULL DEFAULT '' COMMENT '操作人；系统动作为 system',
  `created_at` datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`) /*T![clustered_index] NONCLUSTERED */,
  KEY `idx_run_created` (`run_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin /*T! SHARD_ROW_ID_BITS=4 */ COMMENT='引擎事件日志（只追加）';
