export default {
  tickets: {
    title: '工单管理', description: '处理用户反馈与内部协作', settings: '工单设置',
    buckets: { action_required: '待处理', in_progress: '处理中', waiting_user: '等待用户', ended: '已结束', all: '全部' },
    filters: { search: '搜索工单、用户、请求或订单', category: '全部类型', priority: '全部优先级', assignment: '负责人', allAssignees: '全部负责人', unassigned: '未分配', createdFrom: '创建开始', createdTo: '创建结束' },
    columns: { ticket: '工单', requester: '用户', priority: '优先级', assignee: '负责人', status: '状态', waiting: '等待时间' },
    detail: {
      title: '管理工单', back: '返回工单队列', claim: '接单', unassign: '取消分配', resolve: '标记解决', reopen: '重新打开', close: '关闭工单',
      closeReason: '关闭原因', closeReasonPlaceholder: '说明关闭原因，用户可见', assignment: '负责人', priority: '优先级',
      replyUser: '回复用户', internalNote: '内部备注', internalOnly: '仅管理员可见', bodyPlaceholder: '输入回复内容',
      nextAction: '发送后', waitUser: '等待用户', keepProcessing: '保持处理中', replyAndResolve: '回复并解决', send: '发送',
      conflict: '其他管理员已更新此工单，已刷新详情并保留草稿。', operationFailed: '操作失败'
    },
    storage: {
      title: '工单附件存储', description: '切换模式只影响新上传，不迁移或删除历史附件。',
      modes: { disabled: '关闭附件', local: '本机存储', s3: 'S3 存储' },
      localPath: '本机目录', localWritable: '目录可写', localUnavailable: '目录不可用', sharedVolume: '多实例共享持久卷',
      multiInstanceWarning: '多实例部署必须让所有实例挂载同一个持久卷，否则请勿启用本机存储。',
      endpoint: 'Endpoint', region: 'Region', bucket: 'Bucket', accessKey: 'Access Key', secretKey: 'Secret Key', prefix: '对象前缀', pathStyle: '强制 Path Style',
      secretConfigured: '已配置；留空保留旧密钥', secretPlaceholder: '留空保留当前密钥',
      usage: '历史附件用量', files: '{count} 个文件', test: '测试连接', testing: '正在测试', save: '保存并启用', saving: '正在保存',
      testSuccess: '存储探针测试通过', saveSuccess: '工单附件设置已保存', failed: '存储配置操作失败', destinationInUse: '已有 S3 附件使用当前目的地，不能修改 endpoint、region、bucket、prefix 或 path-style。'
    }
  }
}
