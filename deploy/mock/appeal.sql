-- 本地开发用的 mock：业务方的风控工单表 appeal，以及几条样例数据。
--
-- 这张表属于业务系统，不归平台管理；平台只通过 SQL 数据源（只读从库连接 appeal_ro）查询。
-- 不放在 deploy/sql/ 下，避免被 goctl model 生成到平台的数据访问层。
--
-- 与线上 DDL 的差异：去掉了与本场景无关的索引和列级字符集声明；transaction_created_at 的默认值由 '0000-00-00 00:00:00' 改为 '1970-01-01 00:00:01'，
-- 因为 MySQL 8 默认的 NO_ZERO_DATE 模式不接受零日期。
--
-- 业务口径（待业务方确认，目前按此 mock）：
--   VIP          = vip_level > 0
--   待审核        = appeal_status = 'COMMITED'
--   48h / 7d 起点 = commit_time（秒）

CREATE TABLE IF NOT EXISTS `appeal` (
  `id` int unsigned NOT NULL AUTO_INCREMENT COMMENT '主键id',
  `appeal_type` varchar(128) NOT NULL COMMENT '工单类型',
  `appeal_id` varchar(128) NOT NULL COMMENT '工单id',
  `appeal_status` varchar(128) NOT NULL COMMENT '工单状态（PENDING：待提交，COMMITED：已提交，REJECTED：被驳回，PASS：已通过审核，REFUSE：被拒且不可再次提交，DISUSED：废弃）',
  `documents` varchar(1024) NOT NULL COMMENT '待提交文件类型',
  `uid` bigint NOT NULL COMMENT '用户uid',
  `biz_code` varchar(128) NOT NULL COMMENT '业务类型bizcode',
  `source` varchar(128) NOT NULL COMMENT '来源',
  `channel` varchar(128) NOT NULL COMMENT '渠道',
  `remark` varchar(1028) NOT NULL DEFAULT '' COMMENT '备注',
  `expire_time` bigint NOT NULL COMMENT '过期时间',
  `transaction_id` varchar(128) NOT NULL COMMENT '交易id/请求id',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  `ext` mediumtext NOT NULL COMMENT '扩展信息',
  `reported_uid` bigint NOT NULL DEFAULT '0' COMMENT '被举报用户uid',
  `order_amount` varchar(256) NOT NULL DEFAULT '' COMMENT '订单金额',
  `transaction_created_at` timestamp NOT NULL DEFAULT '1970-01-01 00:00:01' COMMENT '订单创建时间',
  `biz_appeal_status` varchar(256) NOT NULL DEFAULT '' COMMENT '业务工单状态',
  `business` varchar(64) NOT NULL DEFAULT '' COMMENT '业务域',
  `biz_site_id` varchar(64) NOT NULL DEFAULT '' COMMENT '站点id',
  `priority` int NOT NULL DEFAULT '10' COMMENT '优先级',
  `reviewer` varchar(128) NOT NULL DEFAULT '' COMMENT '审核人',
  `case_group` varchar(128) NOT NULL DEFAULT '' COMMENT 'case分组',
  `commit_time` bigint NOT NULL DEFAULT '0' COMMENT '工单提交时间,单位 s',
  `vip_label_type` varchar(64) NOT NULL DEFAULT '' COMMENT 'VIP标签类型',
  `vip_level` int NOT NULL DEFAULT '0' COMMENT 'vip 等级',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uniq_appeal_id` (`appeal_id`),
  KEY `idx_uid` (`uid`),
  KEY `idx_updated_at` (`updated_at`),
  KEY `idx_created_at` (`created_at`),
  KEY `idx_type_site_status_ctime_priority` (`appeal_type`, `biz_site_id`, `appeal_status`, `created_at`, `priority`),
  KEY `idx_reviewer` (`reviewer`),
  KEY `idx_vip_label_type` (`vip_label_type`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci COMMENT = '风控工单（mock）';

-- 样例数据。commit_time 用相对时间，对应 examples/task_types 下三个任务类型的时间窗口：
--   A1001  VIP3，刚提交                → 「VIP 工单待审核」，RM rm.alice
--   A1002  VIP5，提交 50 小时前仍待审核  → 「VIP 工单已等待 48 小时」，RM rm.alice
--   A1003  VIP2，提交 8 天前仍待审核     → 「VIP 工单超过 7 天」，随机分给审核小组的一位组员
--   A1004  VIP4，已审核通过             → 不在任何结果里
--   A1005  非 VIP                      → 不在任何结果里
--   A1006  VIP1，尚未提交（PENDING）     → 不在任何结果里
--   A1007  VIP1，1 小时前提交，uid 没有 RM → 「VIP 工单待审核」，交给兜底人 lead@example.com
INSERT INTO `appeal`
  (`appeal_type`, `appeal_id`, `appeal_status`, `documents`, `uid`, `biz_code`, `source`, `channel`, `expire_time`, `transaction_id`, `ext`,
   `order_amount`, `business`, `biz_site_id`, `reviewer`, `case_group`, `commit_time`, `vip_label_type`, `vip_level`)
VALUES
  ('withdraw_review', 'A1001', 'COMMITED', 'id_card', 10000001, 'fiat_withdraw', 'risk', 'app', 0, 'T9001', '{}', '25000.00', 'fiat', 'global', 'reviewer_a', 'vip_queue', UNIX_TIMESTAMP(),                   'whale', 3),
  ('withdraw_review', 'A1002', 'COMMITED', 'bank_statement', 10000002, 'fiat_withdraw', 'risk', 'app', 0, 'T9002', '{}', '180000.50', 'fiat', 'global', 'reviewer_b', 'vip_queue', UNIX_TIMESTAMP() - 50 * 3600, 'whale', 5),
  ('deposit_review',  'A1003', 'COMMITED', 'proof_of_funds', 10000003, 'fiat_deposit', 'risk', 'web', 0, 'T9003', '{}', '9800.00', 'fiat', 'global', 'reviewer_a', 'vip_queue', UNIX_TIMESTAMP() - 8 * 86400,     'vip', 2),
  ('deposit_review',  'A1004', 'PASS',     'proof_of_funds', 10000004, 'fiat_deposit', 'risk', 'web', 0, 'T9004', '{}', '56000.00', 'fiat', 'global', 'reviewer_c', 'vip_queue', UNIX_TIMESTAMP() - 3 * 86400,    'vip', 4),
  ('withdraw_review', 'A1005', 'COMMITED', 'id_card', 10000005, 'fiat_withdraw', 'risk', 'app', 0, 'T9005', '{}', '120.00', 'fiat', 'global', 'reviewer_b', 'normal_queue', UNIX_TIMESTAMP(),                  '', 0),
  ('withdraw_review', 'A1006', 'PENDING',  'id_card', 10000006, 'fiat_withdraw', 'risk', 'app', 0, 'T9006', '{}', '3000.00', 'fiat', 'global', '', 'vip_queue', 0,                                             'vip', 1),
  ('withdraw_review', 'A1007', 'COMMITED', 'id_card', 10000006, 'fiat_withdraw', 'risk', 'app', 0, 'T9007', '{}', '4200.00', 'fiat', 'global', 'reviewer_c', 'vip_queue', UNIX_TIMESTAMP() - 3600,         'vip', 1);
