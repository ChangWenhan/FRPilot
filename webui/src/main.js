import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import LoginView from './views/LoginView.vue'
import StatusView from './views/StatusView.vue'
import MachinesView from './views/MachinesView.vue'
import MachineDetailView from './views/MachineDetailView.vue'
import TrafficView from './views/TrafficView.vue'
import ActionsView from './views/ActionsView.vue'
import SettingsView from './views/SettingsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView },
    { path: '/', component: StatusView },
    { path: '/machines', component: MachinesView },
    { path: '/machines/:id', component: MachineDetailView },
    { path: '/traffic', component: TrafficView },
    { path: '/actions', component: ActionsView },
    { path: '/settings', component: SettingsView },
  ],
})

createApp(App).use(router).mount('#app')
