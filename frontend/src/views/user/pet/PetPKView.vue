<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { getPetAccounts, getManualPKState, startManualPK, stopManualPK, type ManualPKState, type PetAccount } from '../../../api/pet'

const route=useRoute();const router=useRouter();const accounts=ref<PetAccount[]>([]);const qq=ref(String(route.query.qq||''));const target=ref(10);const state=ref<ManualPKState|null>(null);const loading=ref(false);let timer:ReturnType<typeof setInterval>|null=null
async function load(){if(!qq.value)return;try{state.value=(await getManualPKState(qq.value)).data}catch(e){ElMessage.error(e instanceof Error?e.message:'读取手动 PK 状态失败')}}
async function start(){loading.value=true;try{state.value=(await startManualPK(qq.value,target.value)).data;ElMessage.success('手动 PK 已启动')}catch(e){ElMessage.error(e instanceof Error?e.message:'启动失败')}finally{loading.value=false}}
async function stop(){loading.value=true;try{state.value=(await stopManualPK(qq.value)).data;ElMessage.success('手动 PK 已停止')}catch(e){ElMessage.error(e instanceof Error?e.message:'停止失败')}finally{loading.value=false}}
function formatLogTime(value:string){const date=new Date(value);if(Number.isNaN(date.getTime()))return value.replace('T',' ').replace(/([+-]\d{2}:?\d{2}|Z)$/,'');return new Intl.DateTimeFormat('sv-SE',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'}).format(date)}
onMounted(async()=>{accounts.value=(await getPetAccounts()).data||[];if(!qq.value)qq.value=accounts.value.find(a=>!a.expired)?.qq_number||'';await load();timer=setInterval(()=>void load(),10000)});onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>
<template>
  <div class="page-heading"><div><h1>宠物 PK</h1><p>设置本次目标次数，自动寻找陌生人并完成 PK。</p></div><el-button @click="router.push('/pet/auto-control')">返回自动控制</el-button></div>
  <el-card v-loading="loading"><div class="pet-auto-control-content">
    <el-select v-model="qq" placeholder="选择 QQ 账号" @change="load"><el-option v-for="a in accounts" :key="a.qq_number" :label="`${a.nickname||a.qq_number} · ${a.qq_number}`" :value="a.qq_number" /></el-select>
    <el-alert title="每个陌生人最多 PK 3 次；连续 10 轮找不到可 PK 对手时自动停止。今日次数跨重新开始保留，每日 00:00 重置。" type="info" :closable="false" />
    <div class="pet-auto-control-section"><div class="pet-auto-control-section-title">PK 设置</div><div class="pet-auto-control-plan-fields"><span>目标 PK 次数</span><el-input-number v-model="target" :min="1" :max="500" :precision="0" :disabled="state?.enabled" /><el-button v-if="!state?.enabled" type="primary" :disabled="!qq" @click="start">开始 PK</el-button><el-button v-else type="danger" @click="stop">停止 PK</el-button></div></div>
    <el-descriptions v-if="state" :column="2" border><el-descriptions-item label="运行状态">{{ state.message }}</el-descriptions-item><el-descriptions-item label="当前目标">{{ state.target_starts }} 次</el-descriptions-item><el-descriptions-item label="今日已开始">{{ state.today_starts }} 次</el-descriptions-item><el-descriptions-item label="累计已开始">{{ state.total_starts }} 次</el-descriptions-item><el-descriptions-item label="未找到对手轮次">{{ state.rounds }} / 10</el-descriptions-item><el-descriptions-item label="状态更新">{{ state.updated_at }}</el-descriptions-item></el-descriptions>
    <div class="pet-auto-control-section"><div class="pet-auto-control-section-title">PK 日志</div><el-empty v-if="!state?.logs?.length" description="暂无 PK 日志" :image-size="48" /><div v-else class="pet-auto-control-logs"><div v-for="(log,index) in state.logs" :key="`${log.created_at}-${index}`" class="pet-auto-control-log"><span>{{ log.message }}</span><small>{{ formatLogTime(log.created_at) }}</small></div></div></div>
  </div></el-card>
</template>
