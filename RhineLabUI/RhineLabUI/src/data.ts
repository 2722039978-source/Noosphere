export interface ArchiveRecord {
  id: string;
  title: string;
  en: string;
  department: string;
  category: string;
  date: string;
  lead: string;
  clearance: string;
  abstract: string;
  findings: string[];
  source: string;
}
// Original archive essays informed by public lore; not transcriptions of game files.
export const records: ArchiveRecord[] = [
  {
    id: "X-001",
    title: "莱茵生命",
    en: "RHINE LAB, LLC.",
    department: "总辖构件科",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Kristen Wright / Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "莱茵生命从克丽斯腾与塞雷娅共同创办的研究室发展而来。这里聚集着研究生命、能源与未知技术的人，也承受着资金、权力和成果应用的压力。理解这家机构，需要同时阅读它的实验记录与研究者的选择。",
    findings: [
      "研究方向跨越多个科属，重大项目往往依赖跨科协作。",
      "一项成果的技术价值，与它最终被怎样使用，需要分别讨论。",
      "机构沿革与个人履历交叉编目，保留不同阶段的立场变化。",
    ],
    source: "https://arknights.wiki.gg/wiki/Rhine_Lab",
  },
  {
    id: "X-002",
    title: "结构与边界",
    en: "STRUCTURE & BOUNDARIES",
    department: "工程科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Engineering Section",
    clearance: "REFERENCE AREA",
    abstract:
      "把实验室中的设想变成可运行的设施，需要解决承重、维修和人员通行这些具体问题。莱茵生命的工程人员为大型研究项目提供设备支持。本卷以设施边界为主题，关注构想落地之后由谁维护、由谁承担失效后果。",
    findings: [
      "承载结构与生命科学意义上的结构科分属不同问题。",
      "比较试验装置与长期运行设施对检修空间的不同要求。",
      "为异常停机保留通道，不能只记录设备正常工作的状态。",
    ],
    source: "https://prts.wiki/w/莱茵生命高级工程科成员",
  },
  {
    id: "X-003",
    title: "生命的轮廓",
    en: "THE CONTOUR OF LIFE",
    department: "结构科",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Silence",
    clearance: "REFERENCE AREA",
    abstract:
      "赫默在莱茵生命时期从事与源石有关的生命科学研究，后来成为罗德岛的医疗干员。她的经历让研究对象与患者之间的距离变得具体：一份看似完整的实验结果，未必回答了受试者将如何生活。",
    findings: [
      "把组织变化、功能变化与个人感受放在同一观察框架中。",
      "临床照护记录应与实验阶段的目标和假设相互核对。",
      "研究中的未知应被保留下来，不能被漂亮的结论掩盖。",
    ],
    source: "https://prts.wiki/w/赫默",
  },
  {
    id: "X-004",
    title: "能量转换",
    en: "ENERGY CONVERSION",
    department: "能量科",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Ferdinand Clooney",
    clearance: "REFERENCE AREA",
    abstract:
      "斐尔迪南与莱茵生命能量科、重大项目的资源投入关系密切。大型能源工程也因此同时是一项科研工作和一场组织协作。本卷讨论能量从实验结果走向实际应用时，效率、供给与外部利益之间的关联。",
    findings: [
      "记录能源来源、转换环节和最终用途之间的联系。",
      "规模扩大后，单次实验的成功不能替代持续运行的验证。",
      "项目的资金安排也会改变技术路线与成果使用的方向。",
    ],
    source: "https://prts.wiki/w/多萝西",
  },
  {
    id: "X-005",
    title: "生态观测",
    en: "ECOLOGICAL OBSERVATION",
    department: "生态科",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Muelsyse",
    clearance: "REFERENCE AREA",
    abstract:
      "缪尔赛思领导的生态科研究生命与环境之间的关系，也与罗德岛开展联合科研。洁净的实验环境对于她具有特殊意义。本卷从水体和植物的连续观察出发，追问实验室里的稳定条件能够解释多少真实环境中的变化。",
    findings: [
      "对照水体、植物与环境记录，避免把关联直接当作因果。",
      "记录采样地点和条件，保留无法在实验室中重现的差异。",
      "环境净化既影响研究结果，也关系到研究者自身的需要。",
    ],
    source: "https://prts.wiki/w/缪尔赛思",
  },
  {
    id: "X-006",
    title: "实验安全规范",
    en: "LABORATORY SAFETY",
    department: "防卫科",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "塞雷娅兼具科研背景与异常事件处置能力，曾担任莱茵生命防卫科主任。她的经历提示，实验安全不能只被理解为门禁与装备：当项目目标与具体生命发生冲突时，必须有人拥有叫停研究并保护现场人员的能力。",
    findings: [
      "事前准备应包含撤离路径、联络关系与中止条件。",
      "异常记录需要说明影响到谁，而不只是损坏了哪些设备。",
      "安保与科研人员应共享必要信息，避免风险被科属边界隔断。",
    ],
    source: "https://prts.wiki/w/塞雷娅",
  },
  {
    id: "X-007",
    title: "精密仪器校准",
    en: "INSTRUMENT CALIBRATION",
    department: "工程科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Mayer",
    clearance: "REFERENCE AREA",
    abstract:
      "梅尔曾为赫默参与的项目筹备和调试实验装置，也在鲁特拉工作室持续进行机械设计。仪器把研究者的想法变成可测量的信号；这个过程中的偏差，可能被之后的每一步计算放大。",
    findings: [
      "保留测量基准、设备状态与原始读数之间的对应关系。",
      "设备移交时，解释适用条件与维护方式同样必要。",
      "调试人员掌握的局部经验，应成为项目记录的一部分。",
    ],
    source: "https://prts.wiki/w/梅尔",
  },
  {
    id: "X-008",
    title: "边界之外",
    en: "BEYOND THE HORIZON",
    department: "总辖构件科",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Kristen Wright",
    clearance: "REFERENCE AREA",
    abstract:
      "克丽斯腾把研究的方向指向泰拉天空之外。《孤星》中的探索，将关于天空的疑问变成了牵动整座特里蒙的事件。本卷收录这一主题的观察线索，也保留理想、隐瞒与代价之间难以消解的张力。",
    findings: [
      "将对未知的追求与具体项目的执行方式分别审视。",
      "从参与者能够看到的信息出发，比较他们对同一事件的判断。",
      "突破边界之后，留下的人仍需要面对地面上的后果。",
    ],
    source: "https://arknights.wiki.gg/wiki/Lone_Trail",
  },
  {
    id: "X-009",
    title: "材料记忆",
    en: "MATERIAL MEMORY",
    department: "工程科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Engineering Section",
    clearance: "REFERENCE AREA",
    abstract:
      "动力装甲把材料、动力与控制系统压缩在有限空间里。莱茵生命的 R 系列设备为观察这些约束提供了入口。本卷将反复受力后的材料变化作为研究主题，关注装置看似恢复正常时，内部是否仍保留着上一次使用的痕迹。",
    findings: [
      "把外观完好与内部状态分开记录，避免只依赖目视判断。",
      "比较可更换部件与整体承载部件的维护需求。",
      "疲劳、磨损和重复装配应进入同一份设备履历。",
    ],
    source: "https://prts.wiki/w/R-31重型动力装甲",
  },
  {
    id: "X-010",
    title: "观测者日志",
    en: "OBSERVER’S JOURNAL",
    department: "科考科",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Magallan",
    clearance: "REFERENCE AREA",
    abstract:
      "麦哲伦长期在偏远地区开展探索，冰原是她反复前往的研究场所。一次外勤带回的可能只是一段影像或一件标本，却能为下一位研究者减少一处盲区。本卷关注现场记录如何逐渐累积成可传递的知识。",
    findings: [
      "保留路线、天气与观察位置，说明资料取得时的实际条件。",
      "把尚未解释的现象列为问题，留待后续观测比较。",
      "归档不仅保存成功发现，也保存走错的路与未完成的调查。",
    ],
    source: "https://prts.wiki/w/Magallan",
  },
  {
    id: "X-011",
    title: "信号与噪声",
    en: "SIGNAL & NOISE",
    department: "数据资料组",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Ptilopsis",
    clearance: "REFERENCE AREA",
    abstract:
      "白面鸮曾是莱茵生命的数据维护人员，在罗德岛继续协助医疗记录与信息整理。可被检索的数据并不自动等于可靠的知识。本卷讨论记录、筛选与解释之间的边界，尤其关注异常信号被当作噪声丢弃的可能。",
    findings: [
      "保留原始记录与处理结果，便于重新审视筛选条件。",
      "数据缺失和异常都应明确标注，不能被默认填补。",
      "检索结论需要回到记录发生的情境中解释。",
    ],
    source: "https://prts.wiki/w/白面鸮",
  },
  {
    id: "X-012",
    title: "明日的坐标",
    en: "COORDINATES OF TOMORROW",
    department: "总辖构件科",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Silence / Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "重大事件之后，莱茵生命仍需要决定下一项研究要往哪里去。赫默推动的科学伦理讨论，为这种决定增加了新的尺度。本卷不把未来写成已经完成的答案，而把研究自由、责任和合作视为需要持续协商的问题。",
    findings: [
      "比较技术目标与其希望改善的具体生活之间的距离。",
      "为不同立场留下讨论的位置，记录尚未达成的共识。",
      "项目结束之后，仍应追踪其对人员与环境的长期影响。",
    ],
    source: "https://prts.wiki/w/淬羽赫默",
  },
  {
    id: "X-013",
    title: "共同的起点",
    en: "A SHARED BEGINNING",
    department: "机构沿革组",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Kristen Wright / Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "公开的莱茵生命调查资料将早期研究室追溯至克丽斯腾与塞雷娅的合作。两位创始人拥有相交的志向，也逐渐形成不同的判断。本卷把机构的起点与后来的选择并置，观察共同的理想如何承受现实压力。",
    findings: [
      "以研究室与公司化组织两个阶段梳理机构发展。",
      "区分事后回顾与当时能够掌握的信息。",
      "共同创立机构，并不意味着之后对每一项研究都会达成一致。",
    ],
    source: "https://arknights.wiki.gg/wiki/Rhine_Lab_Research_Report",
  },
  {
    id: "X-014",
    title: "科属之间",
    en: "BETWEEN SECTIONS",
    department: "总辖构件科",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Rhine Lab",
    clearance: "REFERENCE AREA",
    abstract:
      "莱茵生命的工作通过多个科属展开：研究、设备、人员和防卫彼此依赖。赫默后来的岗位变化，也体现了科研决策与组织责任之间的联系。本卷以科属协作为线索，整理信息在机构内部如何传递，又可能在哪里中断。",
    findings: [
      "区分正式职责与重大项目中临时形成的合作关系。",
      "交接记录应说明尚未解决的问题，而不只是已完成的工作。",
      "在项目跨越科属时，明确谁能够提出异议与追问。",
    ],
    source: "https://prts.wiki/w/淬羽赫默",
  },
  {
    id: "X-015",
    title: "特里蒙的日常",
    en: "LIFE IN TRIMOUNTS",
    department: "地区资料组",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Muelsyse",
    clearance: "REFERENCE AREA",
    abstract:
      "特里蒙不仅是科研事件发生的地点，也是研究者工作、社交与生活的城市。缪尔赛思的档案展示了实验室之外的兴趣与城市经验。本卷从这些日常片段观察一座科学都市：宏大的研究计划始终由具体的人参与。",
    findings: [
      "把生活空间与研究空间一并纳入人物背景。",
      "不同身份的人，对同一座城市会有不同的路线与记忆。",
      "保留日常细节，避免人物只剩下职务与成果。",
    ],
    source: "https://prts.wiki/w/缪尔赛思",
  },
  {
    id: "X-016",
    title: "基金会与研究室",
    en: "PATRONS & RESEARCH",
    department: "机构沿革组",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Maylander Foundation",
    clearance: "REFERENCE AREA",
    abstract:
      "莱茵生命的发展与外部支持者并非彼此独立。调查资料提及梅兰德基金会等力量与机构早期成长的联系。本卷关注资源支持如何使研究成为可能，也关注接受资源之后，研究目标与决策空间可能发生的变化。",
    findings: [
      "把资金、设备与人员支持放在同一张项目关系图中。",
      "记录合作条件与研究目标之间可能存在的张力。",
      "对成果用途的讨论，应早于成果正式交付。",
    ],
    source: "https://arknights.wiki.gg/wiki/Rhine_Lab_Research_Report",
  },
  {
    id: "X-017",
    title: "合作的接口",
    en: "POINTS OF CONTACT",
    department: "合作事务组",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Dorothy / Rhodes Island",
    clearance: "REFERENCE AREA",
    abstract:
      "多萝西以莱茵生命与罗德岛合作项目负责人的身份加入合作。机构之间的联系由此落实为具体人员、资料和研究任务。本卷关注合作如何建立信任，也关注双方保有不同立场时，怎样继续进行有限而明确的协作。",
    findings: [
      "把共同研究范围与各自承担的责任写清楚。",
      "人员派驻提供沟通渠道，但不代表机构立场完全一致。",
      "合作关系应允许问题被提出，而不只展示已经达成的成果。",
    ],
    source: "https://prts.wiki/w/多萝西",
  },
  {
    id: "X-018",
    title: "科学与人的位置",
    en: "SCIENCE & ITS PEOPLE",
    department: "总辖构件科",
    category: "机构档案",
    date: "跨期资料汇编",
    lead: "Silence",
    clearance: "REFERENCE AREA",
    abstract:
      "赫默发起《特里蒙科学伦理联合宣言》，并参与相关伦理事务。她把在实验室和临床中面对的问题带到了更广泛的讨论中。本卷围绕一个持续存在的难题展开：科学向前推进时，具体的人如何保有被倾听的位置。",
    findings: [
      "讨论研究对象的处境，不能只讨论研究能够带来什么。",
      "把同意、异议与中止作为持续过程记录。",
      "规范需要在具体案例中反复检验，而非写定之后便不再追问。",
    ],
    source: "https://prts.wiki/w/淬羽赫默",
  },
  {
    id: "X-019",
    title: "咪波的工作台",
    en: "MEEBOO WORKBENCH",
    department: "鲁特拉工作室",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Mayer",
    clearance: "REFERENCE AREA",
    abstract:
      "咪波是梅尔设计和操控的机械装置，也是她倾注感情的作品。从工具到伙伴的距离，在维修、命名与保留旧部件这些动作中逐渐缩短。本卷以工作台为观察位置，讨论创造者如何理解自己制造的机器。",
    findings: [
      "记录设计迭代时保留下来的部件和经验。",
      "把操作者的熟悉程度纳入设备使用条件。",
      "维修记录同时也是一件作品逐渐成形的历史。",
    ],
    source: "https://prts.wiki/w/梅尔",
  },
  {
    id: "X-020",
    title: "龙腾式支援无人机",
    en: "SOARING DRAGON",
    department: "科研设备研发科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Magallan",
    clearance: "REFERENCE AREA",
    abstract:
      "麦哲伦使用的龙腾式支援无人机采用模块化设计，能够适应不同任务。测绘与支援需求在同一套设备上相遇。本卷关注模块化带来的灵活性，以及切换用途时必须重新确认的环境与操作条件。",
    findings: [
      "将任务需求与模块选择对应，保留切换理由。",
      "设备能力必须结合地形、续航和回收条件判断。",
      "同一平台承担多种用途时，应避免把不同模式的表现混为一谈。",
    ],
    source: "https://prts.wiki/w/Magallan",
  },
  {
    id: "X-021",
    title: "R 系列动力装甲",
    en: "R-SERIES PLATFORM",
    department: "工程科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Engineering Section",
    clearance: "REFERENCE AREA",
    abstract:
      "R 系列动力装甲出现在莱茵生命相关实验设施中。工程设备与武装用途在这里直接相接。本卷从维护与使用条件切入，观察复杂设备如何依赖人员协作，也追问研发成果离开工作台之后所获得的新身份。",
    findings: [
      "区分设备待机、维护与投入使用时的不同状态。",
      "将操作者、维修人员和装置作为关联的系统观察。",
      "记录用途变化，避免用最初的研发目标解释所有后续行为。",
    ],
    source: "https://prts.wiki/w/R-31重型动力装甲",
  },
  {
    id: "X-022",
    title: "倾斜的实验室",
    en: "A SHIFTING LABORATORY",
    department: "工程科",
    category: "工程研究",
    date: "跨期资料汇编",
    lead: "Trimounts Laboratory",
    clearance: "REFERENCE AREA",
    abstract:
      "《孤星》中的实验室具有会影响行动的重力环境，机关与可移动物体共同改变空间的使用方式。本卷把这类场景作为设施研究主题：当地面不再是稳定参照时，路径、固定方式和操作习惯都需要重新考虑。",
    findings: [
      "记录环境切换前后，人员与设备能够安全停留的位置。",
      "把移动物体的路径纳入通行与观察安排。",
      "同一空间在不同状态下，应拥有清楚可辨的使用条件。",
    ],
    source: "https://prts.wiki/w/孤星2024",
  },
  {
    id: "X-023",
    title: "伊芙利特的恢复",
    en: "IFRIT: RECOVERY",
    department: "医疗协作组",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Ifrit / Silence",
    clearance: "REFERENCE AREA",
    abstract:
      "伊芙利特曾是莱茵生命的医疗对象，之后在罗德岛由赫默照护。她的故事不应停留在异常能力或实验经历上。本卷把恢复、学习与日常关系放到记录中心，关注她如何逐渐理解自己的力量，并建立自己的生活。",
    findings: [
      "将身体状态与日常适应共同记录，避免只追踪能力表现。",
      "保留当事人的表达，不让旁观者的描述取代她的声音。",
      "区分历史事件造成的影响与正在发生的改变。",
    ],
    source: "https://prts.wiki/w/Ifrit",
  },
  {
    id: "X-024",
    title: "白面鸮的记录方式",
    en: "PTILOPSIS: RECORDS",
    department: "医疗数据组",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Ptilopsis",
    clearance: "REFERENCE AREA",
    abstract:
      "白面鸮以独特的表达方式交流，也继续承担医疗与资料整理工作。她的语言形式并不意味着缺乏情感。本卷关注信息表达与人的经验之间的关系，提醒读者在精确的记录格式之外，仍要理解正在与自己交流的个体。",
    findings: [
      "记录交流所需的时间与条件，避免急于替对方作答。",
      "将表达形式与意图分开理解。",
      "资料维护中的可靠性，来自对细节与具体需求的持续关注。",
    ],
    source: "https://prts.wiki/w/白面鸮",
  },
  {
    id: "X-025",
    title: "治疗抵达之前",
    en: "CARE IN TRANSIT",
    department: "医疗协作组",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Silence",
    clearance: "REFERENCE AREA",
    abstract:
      "赫默提出的医疗无人机以提供治疗为核心目的。它让照护能够抵达人员难以及时靠近的位置，也受到环境与设备条件的限制。本卷从治疗如何被送达这一问题出发，讨论医疗工具与临床判断之间的配合。",
    findings: [
      "区分设备能够抵达的位置与真正能够完成的任务。",
      "把现场条件、响应时间与后续照护连续记录。",
      "工具延伸医生的能力，也需要医生判断何时不能依赖工具。",
    ],
    source: "https://prts.wiki/w/赫默",
  },
  {
    id: "X-026",
    title: "温室与故乡",
    en: "GREENHOUSE & HOME",
    department: "生态科",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Muelsyse",
    clearance: "REFERENCE AREA",
    abstract:
      "缪尔赛思的精灵身份，让环境研究与她自身的处境紧密相连。温室既是研究场所，也承载着对适宜生活空间的追寻。本卷由此讨论保存与改变之间的关系：能够被维持的环境，怎样才能成为真正可以生活的地方。",
    findings: [
      "把环境需求与个人经验同时纳入观察。",
      "区分保存一个样本与延续一种生活所需的条件。",
      "研究可以解释环境变化，却不能代替当事人决定归属。",
    ],
    source: "https://prts.wiki/w/缪尔赛思",
  },
  {
    id: "X-027",
    title: "冰原中的生命线",
    en: "LIFE ON THE ICEFIELD",
    department: "科考科",
    category: "生命科学",
    date: "跨期资料汇编",
    lead: "Magallan",
    clearance: "REFERENCE AREA",
    abstract:
      "麦哲伦的冰原探索不断把远方资料带回研究机构。在人迹稀少的地方，标本、地貌与环境条件需要一起记录。本卷关注如何在信息有限的情况下保留现场细节，使后续研究仍能回到观察发生的地点与情境。",
    findings: [
      "将标本与取得它的环境绑定编目。",
      "为无法重复的观察保留影像、路线和描述。",
      "对未知现象保持开放，同时明确证据能够支持到哪里。",
    ],
    source: "https://prts.wiki/w/Magallan",
  },
  {
    id: "X-028",
    title: "钙质化的应用",
    en: "CALCIFICATION STUDIES",
    department: "源石技艺资料组",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "塞雷娅的技艺与钙及其化合物有关，并与她的医学知识相互配合。防护、支援与环境影响在这一能力中同时出现。本卷将其作为应用研究的案例，关注能力的目的如何决定同一技术被怎样使用。",
    findings: [
      "将能力表现与使用目的、对象和范围联系起来。",
      "比较防护与医疗支援对控制精度的不同要求。",
      "避免仅以破坏力衡量一项源石技艺的价值。",
    ],
    source: "https://prts.wiki/w/塞雷娅",
  },
  {
    id: "X-029",
    title: "源石制品的工序",
    en: "ORIGINIUM WORKFLOWS",
    department: "设备研发组",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Mayer",
    clearance: "REFERENCE AREA",
    abstract:
      "源石相关制品把能源应用带入研究者的日常工作。梅尔的档案提及她在加工中重视安全工序。本卷围绕工作台上的秩序展开：材料如何进入、怎样被加工、使用后又如何被处理，决定了研究能否长期持续。",
    findings: [
      "把材料接收、加工与回收视为连续过程。",
      "设备改装时同步检查原有保护条件是否仍然适用。",
      "记录实际操作中的困难，使工序能够被下一位使用者理解。",
    ],
    source: "https://prts.wiki/w/梅尔",
  },
  {
    id: "X-030",
    title: "装备中的能量预算",
    en: "POWER WITHIN LIMITS",
    department: "工程科／能量科",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Engineering Section",
    clearance: "REFERENCE AREA",
    abstract:
      "工程设备的能力受到可用能源与持续运行条件的共同限制。莱茵生命的装备研发因资源投入而扩展，也必须在实际环境中接受检验。本卷把能量预算作为研究主题，讨论峰值能力与长期可用性之间的取舍。",
    findings: [
      "分别记录启动、持续运行和待机时的需求。",
      "为散热、维护和故障处理保留资源余量。",
      "设备之间共享能源时，应说明优先级与失效时的影响范围。",
    ],
    source: "https://prts.wiki/w/莱茵生命高级工程科成员",
  },
  {
    id: "X-031",
    title: "远距传输链路",
    en: "DISTANT TRANSMISSION",
    department: "能量科",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Trimounts Project",
    clearance: "REFERENCE AREA",
    abstract:
      "《孤星》中的聚焦发生器与地面能量传输相联系。距离增加之后，发射端、接收端与协调工作共同决定系统表现。本卷关注链路的各个环节如何相互依赖，并将中断、偏差与恢复列为同样需要观察的状态。",
    findings: [
      "把发送、接收与控制信号放在同一时间顺序中核对。",
      "链路中断时，区分设备问题与外部条件变化。",
      "不能只用成功到达的结果评价整个传输过程。",
    ],
    source: "https://prts.wiki/w/孤星2024",
  },
  {
    id: "X-032",
    title: "聚焦发生器",
    en: "FOCUS GENERATOR",
    department: "联合项目组",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Kristen Wright",
    clearance: "REFERENCE AREA",
    abstract:
      "聚焦发生器是《孤星》中连接大型工程与高空计划的重要设施。它将远距离获取的能量集中到特定用途，也把设施设计与项目意图紧密结合。本卷从这一装置出发，观察公开目标与参与者所知信息之间的距离。",
    findings: [
      "将装置功能与项目对外叙述分别编目。",
      "追踪关键环节由哪些人员掌握和操作。",
      "讨论单个设备时，同时保留它所属的整体工程背景。",
    ],
    source: "https://prts.wiki/w/孤星2024",
  },
  {
    id: "X-033",
    title: "弧光一号",
    en: "ARCLIGHT ONE",
    department: "能量科／联合项目组",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Ferdinand / Kristen",
    clearance: "REFERENCE AREA",
    abstract:
      "弧光一号把能源设施、空中平台与哥伦比亚的多方力量联系在一起。它的技术规模使单一科属无法独立完成全部工作。本卷关注重大工程中的目标分歧：相同的设备与资源，可能被不同参与者赋予完全不同的期待。",
    findings: [
      "比较研究、军事与个人探索目标之间的重合与偏离。",
      "关键决策应结合参与者当时掌握的信息理解。",
      "保留计划名称之下各项设施与行动之间的具体关系。",
    ],
    source: "https://prts.wiki/w/孤星2024",
  },
  {
    id: "X-034",
    title: "共振装置",
    en: "RESONANCE DEVICES",
    department: "源石技艺应用科",
    category: "能量研究",
    date: "跨期资料汇编",
    lead: "Dorothy Franks",
    clearance: "REFERENCE AREA",
    abstract:
      "多萝西擅长源石技艺的应用，共振装置是理解她研究方向的一条线索。她希望改善他人的处境，但善意并不能自动回答研究方法是否合适。本卷将技术表现与研究动机并列，保留两者之间需要继续追问的部分。",
    findings: [
      "分别记录装置的可观察效果与研究者对它的期待。",
      "参与者的感受与选择，不能被总体目标所覆盖。",
      "对未知机制保持明确标注，避免把愿望写成已经得到验证的结果。",
    ],
    source: "https://prts.wiki/w/多萝西",
  },
  {
    id: "X-035",
    title: "三五九号基地",
    en: "SITE 359",
    department: "项目沿革组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Dorothy / Ferdinand",
    clearance: "REFERENCE AREA",
    abstract:
      "三五九号基地相关事件，是理解《绿野幻梦》与《孤星》联系的重要入口。实验设施之内的研究，与拓荒者的处境和外部利益交织。本卷按参与者与项目关系整理线索，避免只从最终结果倒推每个人的动机。",
    findings: [
      "对照研究者、受试者与资助方能够获取的信息。",
      "将基地中的实验与场外的资源关系一并观察。",
      "一个项目结束之后，其技术与人员关系仍可能延续。",
    ],
    source: "https://arknights.wiki.gg/wiki/Lone_Trail",
  },
  {
    id: "X-036",
    title: "炎魔事件",
    en: "THE DIABOLIC INCIDENT",
    department: "历史事件组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Ifrit / Silence / Saria",
    clearance: "REFERENCE AREA",
    abstract:
      "炎魔事件改变了伊芙利特、赫默与塞雷娅之间的关系，也暴露出莱茵生命研究中的深刻问题。本卷以事件影响为中心，保留照护、误解与责任这些线索；受影响者之后的生活，同样属于事件记录的一部分。",
    findings: [
      "将实验阶段的记录与之后的照护经历相互参照。",
      "明确资料缺失之处，避免用单一解释补齐所有空白。",
      "事件的后果不仅体现为设施损坏，也体现为关系与生活的改变。",
    ],
    source: "https://prts.wiki/w/Ifrit",
  },
  {
    id: "X-037",
    title: "孤星航迹",
    en: "LONE TRAIL",
    department: "高空研究资料组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Kristen Wright",
    clearance: "REFERENCE AREA",
    abstract:
      "《孤星》将克丽斯腾的探索推向天空之外，也让留在地面的人重新判断自己与莱茵生命的关系。本卷以航迹为线索阅读这一事件：每一次向前推进，都伴随着某些联系的延续，也伴随着无法撤回的告别。",
    findings: [
      "把空中的行动与地面的应对按时间相互对照。",
      "保留不同人物对同一时刻的理解。",
      "探索的意义既在于抵达，也在于它改变了后来者能够提出的问题。",
    ],
    source: "https://arknights.wiki.gg/wiki/Lone_Trail",
  },
  {
    id: "X-038",
    title: "观测极限",
    en: "LIMITS OF OBSERVATION",
    department: "高空研究资料组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Rhine Lab",
    clearance: "REFERENCE AREA",
    abstract:
      "关于天空的知识，长期受制于观察者能够到达的位置与能够取得的证据。《孤星》的探索使这些限制成为可以被讨论的问题。本卷不急于替未知命名，而关注一次新的观察如何迫使旧有解释重新接受检验。",
    findings: [
      "区分直接观察、推论与仍待验证的假设。",
      "当新的资料与既有解释冲突时，同时保留双方的依据。",
      "记录观察工具与位置，说明结论受到哪些条件限制。",
    ],
    source: "https://arknights.wiki.gg/wiki/Lone_Trail",
  },
  {
    id: "X-039",
    title: "万星园",
    en: "GALLERIA STELLARIA",
    department: "观星设施资料组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Kristen Wright",
    clearance: "REFERENCE AREA",
    abstract:
      "万星园是克丽斯腾设计的观星空间，中央的星象仪呈现出值得追问的星空线索。它既是一处设施，也显露出设计者的个人追求。本卷关注空间如何承载研究者的想象，以及想象与可证实的观察如何彼此接近。",
    findings: [
      "分别记录星象仪呈现的内容与观测获得的资料。",
      "将设施布置与设计者的研究目标交叉阅读。",
      "一个私人的观星空间，也可能连接着远超个人规模的计划。",
    ],
    source: "https://prts.wiki/w/孤星2024",
  },
  {
    id: "X-040",
    title: "静滞所",
    en: "HALL OF STASIS",
    department: "遗存调查组",
    category: "特别项目",
    date: "跨期资料汇编",
    lead: "Investigation Team",
    clearance: "REFERENCE AREA",
    abstract:
      "静滞所留下的设施与封闭装置，让调查者面对大量无法解释的现象。资料的不完整本身构成了这个地点的重要特征。本卷保留现场、遗存与尚未解决的问题，避免在证据不足时把未知写成完整的历史。",
    findings: [
      "将现场观察与对装置用途的推测分别记录。",
      "无法开启或解释的对象仍应保留原始状态描述。",
      "后续资料可以补充旧记录，但不应抹去最初的疑问。",
    ],
    source: "https://arknights.wiki.gg/wiki/Lone_Trail/Investigation_Reports",
  },
];

export const categories = [
  "全部档案",
  "机构档案",
  "工程研究",
  "生命科学",
  "能量研究",
  "特别项目",
];
export const archiveColumns = [
  "工程研究",
  "生命科学",
  "机构档案",
  "能量研究",
  "特别项目",
];
export function columnFiles(lane: number) {
  return records
    .map((record, index) => ({ record, index }))
    .filter(({ record }) => record.category === archiveColumns[lane])
    .map(({ index }) => index);
}
export function fileLocation(index: number) {
  const lane = archiveColumns.indexOf(records[index].category);
  const row = 12 + columnFiles(lane).indexOf(index);
  return { lane, row, slot: lane * 32 + row };
}
export function fileAtSlot(slot: number) {
  const files = columnFiles(Math.floor(slot / 32));
  return files[Math.max(0, Math.min(files.length - 1, (slot % 32) - 12))];
}
