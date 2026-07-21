export default {
  tickets: {
    title: '工单反馈',
    description: '提交问题并跟踪处理进展',
    newTicket: '新建工单',
    searchPlaceholder: '搜索工单号或主题',
    refresh: '刷新',
    buckets: { all: '全部', active: '进行中', waiting_user: '待我回复', ended: '已结束' },
    status: { open: '待受理', in_progress: '处理中', waiting_user: '待您回复', resolved: '已解决', closed: '已关闭' },
    category: { api_issue: 'API 问题', subscription: '订阅问题', payment: '支付问题', account: '账号问题', feature_request: '功能建议', other: '其他' },
    impact: { blocked: '业务受阻', degraded: '部分受影响', general: '一般咨询' },
    priority: { urgent: '紧急', high: '高', normal: '普通', low: '低' },
    columns: { subject: '主题', status: '状态', category: '类型', updated: '最近更新' },
    empty: '暂无工单',
    emptyDescription: '遇到问题时可创建工单与管理员沟通。',
    unread: '有新回复',
    create: {
      title: '新建工单', category: '问题类型', impact: '影响情况', subject: '主题', description: '问题描述',
      subjectPlaceholder: '简要说明遇到的问题', bodyPlaceholder: '请说明复现步骤、预期结果和实际结果',
      relatedResource: '关联资源', relatedUsage: '关联使用记录', relatedSubscription: '关联订阅（可选）', usageLog: '使用记录 ID', apiKey: 'API 密钥 ID', order: '订单 ID', subscription: '订阅 ID',
      optionalIdPlaceholder: '可选，仅填写属于您的记录 ID', optionalResourcePlaceholder: '可选，不关联也可提交', submit: '提交工单', submitting: '正在提交', cancel: '取消',
      leaveWarning: '当前内容尚未提交，确定离开吗？', failed: '创建工单失败'
    },
    detail: {
      title: '工单详情', back: '返回工单列表', context: '关联信息', conversation: '沟通记录', noConversation: '暂无沟通记录',
      replyPlaceholder: '补充问题信息', send: '发送回复', close: '关闭工单', confirmResolved: '确认解决', reopen: '问题仍未解决',
      reopenPlaceholder: '请说明仍未解决的情况', closedHint: '该工单已关闭，不能继续回复。',
      refreshConflict: '工单已被更新，已刷新详情并保留您的输入。', actionFailed: '操作失败', downloadFailed: '附件下载失败'
    },
    attachments: {
      add: '添加附件', uploading: '正在上传', remove: '移除', disabled: '附件上传已关闭',
      limits: '支持 PNG、JPEG、WebP、TXT、JSON', tooMany: '附件数量超过限制', uploadFailed: '附件上传失败'
    },
    events: {
      ticket_created: '工单已创建', ticket_claimed: '管理员已接单', ticket_assigned: '负责人已更新', priority_changed: '优先级已更新',
      status_changed: '工单状态已更新', ticket_resolved: '工单已标记为解决', ticket_reopened: '工单已重新打开',
      ticket_closed: '工单已关闭', ticket_auto_closed: '工单在重开期限结束后自动关闭'
    },
    time: { justNow: '刚刚' }
  }
}
