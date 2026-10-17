-- 配置版本管理表结构（TiDB / MySQL 兼容）
-- 设计见 docs/ai-task-platform.md「配置生命周期」「版本管理」。
--
-- 模型：
--   * 任务类型是发布单元：schema、来源、workflow、自有卡片模板都内嵌在任务类型的 body 里，随它一起发版；
--   * 共享组件（卡片模板、分配策略、跨部门协议、提示词模板）独立发版，引用时固定版本；
--   * 审批与发布的单位是变更集；审批通过后变更集内所有版本在一个事务内发布；
--   * 任务类型每个版本生成一份发布清单（cfg_manifest_item），相当于锁文件。
-- goctl 约束同 engine.sql：单列主键、AUTO_INCREMENT、created_at / updated_at 由数据库维护。

-- 配置对象：任务类型或共享组件
CREATE TABLE `cfg_component` (
  `id`             bigint unsigned NOT NULL AUTO_INCREMENT,
  `namespace`      varchar(64)  NOT NULL COMMENT '业务线或团队；v1 只用于分组与筛选，不做数据级权限',
  `kind`           varchar(32)  NOT NULL COMMENT 'task_type / view / assignment / agreement / prompt',
  `comp_key`       varchar(64)  NOT NULL COMMENT '稳定标识，如 vip_ticket_alert、compliance_card',
  `name`           varchar(128) NOT NULL,
  `description`    varchar(1024) NOT NULL DEFAULT '',
  `latest_version` int          NOT NULL DEFAULT 0 COMMENT '最新已发布版本；0 = 尚未发布',
  `status`         varchar(16)  NOT NULL DEFAULT 'active' COMMENT 'active / retired',
  `created_by`     varchar(64)  NOT NULL DEFAULT '' COMMENT '人或 AI，如 ai:openclaw',
  `created_at`     datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`     datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_kind_key` (`kind`, `comp_key`),
  KEY `idx_namespace_kind` (`namespace`, `kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='配置对象（任务类型与共享组件）';

-- 草稿：每个配置对象同时最多一份
CREATE TABLE `cfg_draft` (
  `id`            bigint unsigned NOT NULL AUTO_INCREMENT,
  `component_id`  bigint unsigned NOT NULL,
  `base_version`  int          NOT NULL DEFAULT 0 COMMENT '基于哪个已发布版本创建；0 = 全新',
  `body`          json         NOT NULL COMMENT '任务类型草稿含 schema / sources / workflow / views 等自有组件',
  `revision`      bigint       NOT NULL DEFAULT 0 COMMENT '乐观锁：保存时必须携带，冲突返回错误',
  `change_set_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT '非 0 表示已提交审批、锁定不可编辑',
  `updated_by`    varchar(64)  NOT NULL DEFAULT '',
  `created_at`    datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`    datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_component` (`component_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='配置草稿';

-- 已发布版本：不可变
CREATE TABLE `cfg_version` (
  `id`            bigint unsigned NOT NULL AUTO_INCREMENT,
  `component_id`  bigint unsigned NOT NULL,
  `version`       int          NOT NULL,
  `body`          json         NOT NULL,
  `content_hash`  varchar(64)  NOT NULL COMMENT 'sha256(body)',
  `state`         varchar(16)  NOT NULL DEFAULT 'published' COMMENT 'published / retired',
  `change_set_id` bigint unsigned NOT NULL COMMENT '随哪个变更集发布',
  `published_by`  varchar(64)  NOT NULL DEFAULT '' COMMENT '变更集起草人',
  `approved_by`   varchar(64)  NOT NULL DEFAULT '' COMMENT '审批平台回调中的审批人',
  `published_at`  datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `retired_at`    datetime(3)  NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_component_version` (`component_id`, `version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='已发布配置版本';

-- 变更集：审批与发布的单位
CREATE TABLE `cfg_change_set` (
  `id`                   bigint unsigned NOT NULL AUTO_INCREMENT,
  `namespace`            varchar(64)  NOT NULL,
  `title`                varchar(256) NOT NULL,
  `state`                varchar(16)  NOT NULL COMMENT 'open / submitted / published / rejected / withdrawn',
  `approval_instance_id` varchar(128) NOT NULL DEFAULT '' COMMENT '公司审批平台的实例 ID',
  `created_by`           varchar(64)  NOT NULL DEFAULT '',
  `decided_by`           varchar(64)  NOT NULL DEFAULT '',
  `decision_comment`     varchar(1024) NOT NULL DEFAULT '',
  `submitted_at`         datetime(3)  NULL,
  `decided_at`           datetime(3)  NULL,
  `created_at`           datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`           datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_namespace_state` (`namespace`, `state`),
  KEY `idx_approval_instance` (`approval_instance_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='变更集';

-- 变更集条目：提交时对草稿做快照，审批的内容即发布的内容
CREATE TABLE `cfg_change_set_item` (
  `id`             bigint unsigned NOT NULL AUTO_INCREMENT,
  `change_set_id`  bigint unsigned NOT NULL,
  `component_id`   bigint unsigned NOT NULL,
  `draft_revision` bigint       NOT NULL COMMENT '加入或提交时的草稿 revision',
  `base_version`   int          NOT NULL DEFAULT 0,
  `target_version` int          NOT NULL COMMENT '发布后的版本号 = latest_version + 1',
  `body_snapshot`  json         NULL COMMENT '提交时的草稿快照；发布使用快照而不是当前草稿',
  `content_hash`   varchar(64)  NOT NULL DEFAULT '',
  `reason`         varchar(32)  NOT NULL DEFAULT 'edit' COMMENT 'edit / dependent_upgrade',
  `created_at`     datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_change_set_component` (`change_set_id`, `component_id`),
  KEY `idx_component` (`component_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='变更集条目';

-- 发布清单：任务类型某个版本的完整依赖闭包（锁文件）
CREATE TABLE `cfg_manifest_item` (
  `id`                 bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_type_id`       bigint unsigned NOT NULL COMMENT 'cfg_component.id（kind = task_type）',
  `task_type_version`  int          NOT NULL,
  `item_kind`          varchar(32)  NOT NULL COMMENT 'schema / source / workflow / view / assignment / agreement / prompt',
  `item_key`           varchar(64)  NOT NULL COMMENT '清单内的引用名，如 rm_card、compliance_card',
  `ref_component_id`   bigint unsigned NOT NULL DEFAULT 0 COMMENT '共享组件 ID；0 = 自有组件',
  `ref_version`        int          NOT NULL DEFAULT 0 COMMENT '共享组件的固定版本；自有组件为 0',
  `content_hash`       varchar(64)  NOT NULL,
  `created_at`         datetime(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_manifest_item` (`task_type_id`, `task_type_version`, `item_kind`, `item_key`),
  KEY `idx_ref` (`ref_component_id`, `ref_version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin COMMENT='发布清单';
