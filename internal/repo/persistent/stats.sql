WITH team_tasks AS (
    SELECT id, status, assignee_id, created_at, closed_at
    FROM tasks
    WHERE team_id = ?
), status_counts AS (
    SELECT status, COUNT(*) AS task_count
    FROM team_tasks
    GROUP BY status
), top_assignees AS (
    SELECT u.id AS user_id, u.name, COUNT(*) AS closed_tasks
    FROM team_tasks t
    JOIN users u ON u.id = t.assignee_id
    WHERE t.status = 'done'
      AND t.closed_at >= UTC_TIMESTAMP(6) - INTERVAL 30 DAY
      AND t.closed_at <= UTC_TIMESTAMP(6)
    GROUP BY u.id, u.name
    ORDER BY closed_tasks DESC, u.id ASC
    LIMIT 3
), closure AS (
    SELECT AVG(TIMESTAMPDIFF(MICROSECOND, created_at, closed_at) / 1000000.0) AS avg_seconds
    FROM team_tasks
    WHERE status = 'done'
), comments AS (
    SELECT COUNT(*) AS comment_count
    FROM task_comments c
    JOIN team_tasks t ON t.id = c.task_id
)
SELECT 'status' AS kind, status AS label, CAST(NULL AS SIGNED) AS user_id,
       task_count AS amount, CAST(NULL AS DECIMAL(30,6)) AS avg_seconds
FROM status_counts
UNION ALL
SELECT 'assignee', name, user_id, closed_tasks, NULL FROM top_assignees
UNION ALL
SELECT 'summary', NULL, NULL, comments.comment_count, closure.avg_seconds
FROM comments CROSS JOIN closure;
