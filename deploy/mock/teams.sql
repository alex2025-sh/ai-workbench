SET NAMES utf8mb4;
-- 本地开发用的团队与成员（平台库）。线上在「团队」页面维护。
--   vip_cs_leads  VIP 客服组长：认领模式，通知发到群 oc_vip_cs_leads
--   vip_review    VIP 审核小组：分配模式，组长 lead@example.com 分配，组员执行
INSERT INTO `team` (`team_key`, `name`, `chat_id`, `updated_by`) VALUES
  ('vip_cs_leads', 'VIP 客服组长', 'oc_vip_cs_leads', 'seed'),
  ('vip_review', 'VIP 审核小组', '', 'seed');

INSERT INTO `team_member` (`team_key`, `user_id`, `role`) VALUES
  ('vip_cs_leads', 'lead@example.com', 'executor'),
  ('vip_cs_leads', 'lead.carol@example.com', 'executor'),
  ('vip_review', 'lead@example.com', 'assigner'),
  ('vip_review', 'reviewer.dave@example.com', 'executor'),
  ('vip_review', 'reviewer.erin@example.com', 'executor');
