export default {
  codexTickets: {
    title: 'Codex 打票',
    description: '为 Codex OAuth 账号后台打票（x-codex-turn-state），缺票时可按「全站共用票」借用其他账号的有效票。',
    settings: {
      title: '打票设置',
      enabled: '启用自动打票',
      enabledHint: '开启后后台按间隔为可调度账号打票，并在业务请求中注入票据。关闭后不产生任何打票请求。',
      shared: '全站共用票',
      sharedHint: '账号没有自己的有效票时，允许借用全站其他账号的票据；优先使用自己的票。',
      failClosed: '无票时拒绝请求',
      failClosedHint: '开启后，进入打票范围但既无自己的票、也无可借票的请求会直接失败；默认关闭，按原链路放行。',
      models: '打票模型',
      modelsHint: '逗号分隔。只有列出的模型会打票与注入；留空表示不启用打票与注入。',
      interval: '打票间隔（秒）',
      intervalHint: '后台两轮打票之间的最小间隔，范围 30–3600 秒。',
      ttl: '票据有效期（秒）',
      ttlHint: '票据最长保留时间，范围 60–86400 秒；实际过期时间还会受票据自身签发时间限制。',
      probeTimeout: '单次打票超时（秒）',
      probeTimeoutHint: '单次上游探测的超时时间，范围 5–120 秒。'
    },
    actions: {
      save: '保存设置',
      saving: '保存中…',
      refresh: '刷新',
      harvestAll: '立即打票',
      harvest: '打票',
      harvesting: '打票中…',
      iqTest: '智商测试',
      iqTesting: '测试中…'
    },
    status: {
      ready: '有票',
      own: '自己的票',
      shared: '共用票',
      missing: '无票',
      expiresIn: '剩余 {time}',
      source: '来源 #{id}',
      iqHealthy: '满血',
      iqDegraded: '降智',
      iqInconclusive: '无法判断'
    },
    pool: {
      title: '全站共享票池',
      empty: '共享池暂无可用票据。开启自动打票或手动打票后会出现在这里。',
      model: '模型',
      source: '来源账号',
      remaining: '剩余',
      length: '票长',
      captured: '捕获时间'
    },
    accounts: {
      title: '账号票据状态',
      empty: '没有可打票的 Codex OAuth 账号。',
      account: '账号',
      tickets: '票据',
      actions: '操作'
    },
    messages: {
      saved: '打票设置已保存',
      harvested: '打票完成：成功 {ok}，失败 {failed}，跳过 {skipped}',
      failed: '操作失败：{error}',
      loadFailed: '加载打票状态失败：{error}',
      noModels: '请先配置至少一个打票模型',
      iqDone: '智商测试完成：{verdict}（{ms} ms）'
    },
    iq: {
      title: '智商测试',
      weight: '智商',
      latency: '耗时 {ms} ms',
      ticket: '票长 {length}',
      newTicket: '续接回了新票',
      shortTicket: '短票形态，结论仅供参考',
      testedAt: '测试于 {time}',
      reason: '判据'
    },
    manage: {
      title: '门票管理',
      description: '查看、刷新与删除全站 Codex 门票；删除后下次请求或下一轮打票会重新生成。',
      filters: {
        accountId: '账号 ID',
        accountIdPlaceholder: '全部账号',
        model: '模型',
        modelPlaceholder: '全部模型',
        status: '状态',
        allStatus: '全部状态',
        valid: '有效',
        expired: '已过期',
        search: '查询',
        reset: '重置'
      },
      table: {
        id: 'ID',
        model: '模型',
        account: '账号',
        status: '状态',
        length: '票长',
        acquisition: '来源',
        captured: '捕获时间',
        expires: '过期时间',
        remaining: '剩余',
        actions: '操作'
      },
      actions: {
        refresh: '重新打票',
        refreshing: '打票中…',
        delete: '删除',
        deleting: '删除中…'
      },
      status: {
        valid: '有效',
        expired: '已过期'
      },
      messages: {
        deleted: '门票已删除',
        refreshed: '重新打票完成：成功 {ok}，失败 {failed}',
        refreshFailed: '重新打票失败：{error}',
        deleteFailed: '删除失败：{error}',
        loadFailed: '加载门票失败：{error}',
        deleteConfirm: '确定删除这张门票吗？删除后账号会在下次请求或下一轮打票时重新生成。'
      },
      pagination: {
        prev: '上一页',
        next: '下一页',
        info: '第 {page} / {pages} 页 · 共 {total} 张'
      },
      empty: '暂无门票'
    },
    lastRound: '最近一轮：{time}',
    never: '尚未打票'
  }
}
