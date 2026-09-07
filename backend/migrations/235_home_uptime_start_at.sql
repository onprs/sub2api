-- 首页累计运行时间起点。
-- 使用数据库初始化时的当年 5 月 1 日，并以 setting 记录，避免前端版本更新或
-- 浏览器缓存变化导致累计时间被重置。
INSERT INTO settings (key, value, updated_at)
VALUES (
    'home_uptime_start_at',
    to_char(make_date(EXTRACT(YEAR FROM CURRENT_DATE)::int, 5, 1), 'YYYY-MM-DD"T"00:00:00"Z"'),
    NOW()
)
ON CONFLICT (key) DO NOTHING;
